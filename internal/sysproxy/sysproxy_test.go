package sysproxy

import (
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
