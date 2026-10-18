package service_test

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Sesame2/gotun/internal/config"
	"github.com/Sesame2/gotun/internal/service"
	"github.com/Sesame2/gotun/internal/testutil"
)

func TestProxyServiceInvalidConfigAndRecovery(t *testing.T) {
	s := service.NewProxyService()
	if s.IsRunning() || s.GetStatus().Status != service.StatusStopped {
		t.Fatal("new service is running")
	}
	if err := s.Start(); err == nil {
		t.Fatal("empty SSH configuration accepted")
	}
	if status := s.GetStatus(); status.Status != service.StatusError || status.ErrorMessage == "" {
		t.Fatalf("invalid config status = %+v", status)
	}
	if err := s.Stop(); err != nil {
		t.Fatal(err)
	}
	server := testutil.NewSSHServer(t)
	s.SetConfig(server.Config())
	t.Cleanup(func() { s.Stop() })
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	if !s.IsRunning() || s.GetStatus().ErrorMessage != "" {
		t.Fatal("service did not recover from invalid config")
	}
}

func TestProxyServiceReportsBindFailuresSynchronously(t *testing.T) {
	for _, protocol := range []string{"HTTP", "SOCKS5"} {
		t.Run(protocol, func(t *testing.T) {
			occupied, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer occupied.Close()
			server := testutil.NewSSHServer(t)
			cfg := server.Config()
			if protocol == "HTTP" {
				cfg.ListenAddr = occupied.Addr().String()
			} else {
				cfg.SocksAddr = occupied.Addr().String()
			}
			s := service.NewProxyService()
			s.SetConfig(cfg)
			t.Cleanup(func() { s.Stop() })
			if err := s.Start(); err == nil {
				t.Fatal("Start reported success while the proxy port was already occupied")
			}
			if s.IsRunning() || s.GetStatus().Status != service.StatusError {
				t.Fatalf("bind failure status = %+v", s.GetStatus())
			}
		})
	}
}

func TestProxyServiceStartStopRestartAndTraffic(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "service works") }))
	defer origin.Close()
	server := testutil.NewSSHServer(t)
	cfg := server.Config()
	cfg.ListenAddr = testutil.AvailableAddr(t)
	cfg.SocksAddr = testutil.AvailableAddr(t)
	s := service.NewProxyService()
	s.SetConfig(cfg)
	t.Cleanup(func() { s.Stop() })
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(); err == nil {
		t.Fatal("double start accepted")
	}
	testutil.WaitTCP(t, cfg.ListenAddr)
	testutil.WaitTCP(t, cfg.SocksAddr)
	proxyURL, _ := url.Parse("http://" + cfg.ListenAddr)
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
	resp, err := client.Get(strings.Replace(origin.URL, "127.0.0.1", "remote.test", 1))
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || string(body) != "service works" {
		t.Fatalf("proxy response = %q, %v", body, err)
	}
	if err := s.Restart(); err != nil {
		t.Fatal(err)
	}
	testutil.WaitTCP(t, cfg.ListenAddr)
	if err := s.Stop(); err != nil {
		t.Fatal(err)
	}
	if err := s.Stop(); err != nil {
		t.Fatal(err)
	}
	if status := s.GetStatus(); status.Status != service.StatusStopped || status.Uptime != "" || !status.StartTime.IsZero() {
		t.Fatalf("stopped status = %+v", status)
	}
	for _, addr := range []string{cfg.ListenAddr, cfg.SocksAddr} {
		if conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond); err == nil {
			conn.Close()
			t.Fatalf("listener %s remained open", addr)
		}
	}
}

func TestProxyServiceConnectionTest(t *testing.T) {
	server := testutil.NewSSHServer(t)
	s := service.NewProxyService()
	if err := s.TestConnection(server.Config()); err != nil {
		t.Fatal(err)
	}
	cfg := config.NewConfig()
	cfg.SSHServer = server.Addr
	cfg.SSHUser = "wrong-user"
	cfg.SSHPassword = "wrong-password"
	cfg.SSHKeyFile = server.KeyFile
	cfg.InteractiveAuth = false
	if err := s.TestConnection(cfg); err == nil {
		t.Fatal("invalid SSH test connection succeeded")
	}
	if s.IsRunning() {
		t.Fatal("testing SSH connection started proxy service")
	}
}

func TestProxyServiceClearsRulesWhenSwitchingProfiles(t *testing.T) {
	server := testutil.NewSSHServer(t)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "allowed") }))
	defer origin.Close()
	rules := filepath.Join(t.TempDir(), "rules.yaml")
	if err := os.WriteFile(rules, []byte("rules:\n  - MATCH,REJECT\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s := service.NewProxyService()
	t.Cleanup(func() { s.Stop() })
	for i, want := range []int{http.StatusForbidden, http.StatusOK} {
		cfg := server.Config()
		cfg.ListenAddr = testutil.AvailableAddr(t)
		if i == 0 {
			cfg.RuleFile = rules
		}
		s.SetConfig(cfg)
		if err := s.Start(); err != nil {
			t.Fatal(err)
		}
		testutil.WaitTCP(t, cfg.ListenAddr)
		u, _ := url.Parse("http://" + cfg.ListenAddr)
		transport := &http.Transport{Proxy: http.ProxyURL(u)}
		client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
		resp, err := client.Get(origin.URL)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		transport.CloseIdleConnections()
		if resp.StatusCode != want {
			t.Errorf("profile %d returned %d, want %d", i, resp.StatusCode, want)
		}
		if err := s.Stop(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestProxyServiceRejectedStartPreservesActiveProfile(t *testing.T) {
	server := testutil.NewSSHServer(t)
	s := service.NewProxyService()
	t.Cleanup(func() { s.Stop() })
	first := server.Config()
	if err := s.StartWithConfig(first); err != nil {
		t.Fatal(err)
	}
	other := server.Config()
	other.SSHServer = "other.example:22"
	other.ListenAddr = "127.0.0.1:9999"
	if err := s.StartWithConfig(other); err == nil {
		t.Fatal("running service accepted another profile")
	}
	s.SetConfig(other)
	if status := s.GetStatus(); status.SSHServer != first.SSHServer || status.HTTPAddr != first.ListenAddr {
		t.Fatalf("rejected request replaced active profile: %+v", status)
	}
}

func TestProxyServiceConfigAndLifecycleConcurrency(t *testing.T) {
	s := service.NewProxyService()
	var workers sync.WaitGroup
	for i := 0; i < 4; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for j := 0; j < 30; j++ {
				s.SetConfig(config.NewConfig())
				s.Start()
				s.GetStatus()
				s.Stop()
			}
		}()
	}
	workers.Wait()
}
