package sysproxy

import (
	"errors"
	"strings"
	"testing"

	"github.com/Sesame2/gotun/internal/logger"
)

// Validate normalization without executing any operating system commands.
func TestSystemProxyAddressNormalization(t *testing.T) {
	for _, tc := range []struct{ name, http, socks, wantHTTP, wantSOCKS string }{
		{"empty host", ":8080", ":1080", "127.0.0.1:8080", "127.0.0.1:1080"},
		{"wildcard IPv4", "0.0.0.0:8081", "0.0.0.0:1081", "127.0.0.1:8081", "127.0.0.1:1081"},
		{"explicit host", "127.0.0.1:8888", "localhost:9999", "127.0.0.1:8888", "localhost:9999"},
		{"IPv6", "[::1]:8080", "[2001:db8::1]:1080", "[::1]:8080", "[2001:db8::1]:1080"},
		{"wildcard IPv6", "[::]:8080", "[::]:1080", "127.0.0.1:8080", "127.0.0.1:1080"},
		{"invalid fallback", "invalid", "invalid", "127.0.0.1:8080", ""},
		{"SOCKS disabled", ":8080", "", "127.0.0.1:8080", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewManager(logger.NewLogger(false), tc.http, tc.socks)
			if m.httpAddr != tc.wantHTTP || m.socksAddr != tc.wantSOCKS {
				t.Fatalf("addresses = %q, %q; want %q, %q", m.httpAddr, m.socksAddr, tc.wantHTTP, tc.wantSOCKS)
			}
			if m.IsEnabled() {
				t.Fatal("new manager reports enabled")
			}
			if err := m.Disable(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestExplicitClearAcrossPlatforms(t *testing.T) {
	for _, platform := range []string{"darwin", "windows"} {
		t.Run(platform, func(t *testing.T) {
			m := NewManager(logger.NewLogger(false), ":8080", "")
			m.platform = platform
			var calls []string
			m.run = func(name string, args ...string) ([]byte, error) {
				calls = append(calls, name+" "+strings.Join(args, " "))
				if len(args) > 0 && args[0] == "-listallnetworkservices" {
					return []byte("An asterisk denotes disabled services.\nWi-Fi\nEthernet\n*Disabled\n"), nil
				}
				return nil, nil
			}
			if err := m.Clear(); err != nil {
				t.Fatal(err)
			}
			joined := strings.Join(calls, "\n")
			if platform == "darwin" {
				for _, service := range []string{"Wi-Fi", "Ethernet"} {
					for _, kind := range []string{"web", "secureweb", "socksfirewall"} {
						if !strings.Contains(joined, "-set"+kind+"proxystate "+service+" off") {
							t.Fatal(joined)
						}
					}
				}
			} else if !strings.Contains(joined, "ProxyEnable /t REG_DWORD /d 0") || !strings.Contains(joined, "powershell") {
				t.Fatal(joined)
			}
		})
	}
}

func TestClearReportsErrorsAndAttemptsEveryMacProxy(t *testing.T) {
	for _, platform := range []string{"darwin", "windows"} {
		t.Run(platform, func(t *testing.T) {
			m := NewManager(logger.NewLogger(false), ":8080", "")
			m.platform = platform
			m.enabled = true
			count := 0
			m.run = func(name string, args ...string) ([]byte, error) {
				if len(args) > 0 && args[0] == "-listallnetworkservices" {
					return []byte("Services\nWi-Fi\nEthernet\n"), nil
				}
				count++
				return nil, errors.New("denied")
			}
			if m.Clear() == nil || !m.IsEnabled() {
				t.Fatal("clear failure not propagated or state falsely disabled")
			}
			if platform == "darwin" && count != 6 {
				t.Fatalf("only %d disable commands attempted", count)
			}
		})
	}
}

func TestWindowsNotificationFailure(t *testing.T) {
	m := NewManager(logger.NewLogger(false), ":8080", "")
	m.platform = "windows"
	m.run = func(name string, args ...string) ([]byte, error) {
		if name == "powershell" {
			return nil, errors.New("notify failed")
		}
		return nil, nil
	}
	if m.Clear() == nil {
		t.Fatal("notification failure ignored")
	}
}

func TestPartialEnableRollsBack(t *testing.T) {
	for _, platform := range []string{"darwin", "windows"} {
		t.Run(platform, func(t *testing.T) {
			m := NewManager(logger.NewLogger(false), ":8080", ":1080")
			m.platform = platform
			var calls []string
			m.run = func(name string, args ...string) ([]byte, error) {
				call := name + " " + strings.Join(args, " ")
				calls = append(calls, call)
				if len(args) > 0 && args[0] == "-listallnetworkservices" {
					return []byte("Services\nWi-Fi\n"), nil
				}
				if strings.Contains(call, "-setsecurewebproxy ") || strings.Contains(call, "ProxyServer /t") {
					return nil, errors.New("enable failed")
				}
				return nil, nil
			}
			if m.Enable() == nil {
				t.Fatal("partial enable reported success")
			}
			if m.IsEnabled() {
				t.Fatal("rollback failed to disable")
			}
			joined := strings.Join(calls, "\n")
			if platform == "darwin" && !strings.Contains(joined, "-setwebproxystate Wi-Fi off") {
				t.Fatal(joined)
			}
			if platform == "windows" && !strings.Contains(joined, "ProxyEnable /t REG_DWORD /d 0") {
				t.Fatal(joined)
			}
		})
	}
}

func TestManagerEnableDisableAreIdempotent(t *testing.T) {
	for _, platform := range []string{"darwin", "windows"} {
		t.Run(platform, func(t *testing.T) {
			m := NewManager(logger.NewLogger(false), ":8080", ":1080")
			m.platform = platform
			calls := 0
			m.run = func(name string, args ...string) ([]byte, error) {
				calls++
				if len(args) > 0 && args[0] == "-listallnetworkservices" {
					return []byte("Services\nWi-Fi\n"), nil
				}
				return nil, nil
			}
			if err := m.Enable(); err != nil {
				t.Fatal(err)
			}
			if !m.IsEnabled() {
				t.Fatal("Enable did not update status")
			}
			afterEnable := calls
			if err := m.Enable(); err != nil || calls != afterEnable {
				t.Fatal("Enable not idempotent")
			}
			if err := m.Disable(); err != nil {
				t.Fatal(err)
			}
			if m.IsEnabled() {
				t.Fatal("Disable did not update status")
			}
			afterDisable := calls
			if err := m.Disable(); err != nil || calls != afterDisable {
				t.Fatal("Disable not idempotent")
			}
		})
	}
}
