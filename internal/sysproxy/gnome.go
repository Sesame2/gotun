package sysproxy

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

var gnomeProxyKeys = [][2]string{
	{"org.gnome.system.proxy", "mode"},
	{"org.gnome.system.proxy", "ignore-hosts"},
	{"org.gnome.system.proxy.http", "host"}, {"org.gnome.system.proxy.http", "port"},
	{"org.gnome.system.proxy.https", "host"}, {"org.gnome.system.proxy.https", "port"},
	{"org.gnome.system.proxy.socks", "host"}, {"org.gnome.system.proxy.socks", "port"},
}

func (m *Manager) snapshotLinux() (func() error, error) {
	values := make([]string, len(gnomeProxyKeys))
	for i, key := range gnomeProxyKeys {
		output, err := m.run("gsettings", "get", key[0], key[1])
		if err != nil {
			return nil, fmt.Errorf("读取GNOME代理失败: %w", err)
		}
		values[i] = strings.TrimSpace(string(output))
		if values[i] == "" {
			return nil, fmt.Errorf("GNOME代理快照为空")
		}
	}
	type environmentValue struct {
		value   string
		present bool
	}
	environment := make(map[string]environmentValue)
	for _, name := range []string{"http_proxy", "https_proxy", "no_proxy"} {
		value, present := os.LookupEnv(name)
		environment[name] = environmentValue{value, present}
	}
	return func() error {
		var failures []error
		// Restore hosts and ports before reactivating the previous mode.
		for i := len(gnomeProxyKeys) - 1; i >= 0; i-- {
			key := gnomeProxyKeys[i]
			if _, err := m.run("gsettings", "set", key[0], key[1], values[i]); err != nil {
				failures = append(failures, err)
			}
		}
		for name, value := range environment {
			var err error
			if value.present {
				err = os.Setenv(name, value.value)
			} else {
				err = os.Unsetenv(name)
			}
			failures = append(failures, err)
		}
		return errors.Join(failures...)
	}, nil
}

func (m *Manager) enableLinux() error {
	host, port, _ := net.SplitHostPort(m.httpAddr)
	values := []string{"'manual'", "[]", strconv.Quote(host), port, strconv.Quote(host), port, "''", "0"}
	if m.socksAddr != "" {
		shost, sport, _ := net.SplitHostPort(m.socksAddr)
		values[6], values[7] = strconv.Quote(shost), sport
	}
	for i := len(gnomeProxyKeys) - 1; i >= 0; i-- {
		key := gnomeProxyKeys[i]
		if _, err := m.run("gsettings", "set", key[0], key[1], values[i]); err != nil {
			return fmt.Errorf("设置GNOME代理失败: %w", err)
		}
	}
	return errors.Join(os.Setenv("http_proxy", "http://"+m.httpAddr), os.Setenv("https_proxy", "http://"+m.httpAddr), os.Setenv("no_proxy", ""))
}

func (m *Manager) clearLinux() error {
	if _, err := m.run("gsettings", "set", "org.gnome.system.proxy", "mode", "'none'"); err != nil {
		return fmt.Errorf("清除GNOME代理失败: %w", err)
	}
	return errors.Join(os.Unsetenv("http_proxy"), os.Unsetenv("https_proxy"), os.Unsetenv("no_proxy"))
}
