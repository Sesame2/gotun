package proxy

import (
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Sesame2/gotun/internal/logger"
	"github.com/Sesame2/gotun/internal/router"
	"github.com/Sesame2/gotun/internal/testutil"
	xproxy "golang.org/x/net/proxy"
)

func testSOCKSProxy(t *testing.T, rules string) (string, *testutil.SSHServer) {
	t.Helper()
	server := testutil.NewSSHServer(t)
	cfg := server.Config()
	cfg.SocksAddr = "127.0.0.1:0"
	client, err := NewSSHClient(cfg, logger.NewLogger(false))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	var r *router.Router
	if rules != "" {
		r = testRouter(t, rules)
	}
	p, err := NewSOCKS5OverSSH(cfg, logger.NewLogger(false), client, r)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- p.Start() }()
	t.Cleanup(func() {
		p.Close()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("SOCKS shutdown: %v", err)
			}
		case <-time.After(3 * time.Second):
			t.Error("SOCKS server did not stop")
		}
	})
	addr := p.listener.Addr().String()
	testutil.WaitTCP(t, addr)
	return addr, server
}

func TestSOCKSForwarding(t *testing.T) {
	for _, mode := range []string{"proxy", "direct"} {
		t.Run(mode, func(t *testing.T) {
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "via SOCKS") }))
			defer origin.Close()
			rules := ""
			if mode == "direct" {
				rules = "mode: direct\n"
			}
			addr, server := testSOCKSProxy(t, rules)
			dialer, err := xproxy.SOCKS5("tcp", addr, nil, &net.Dialer{Timeout: time.Second})
			if err != nil {
				t.Fatal(err)
			}
			target := strings.TrimPrefix(origin.URL, "http://")
			if mode == "proxy" {
				target = strings.Replace(target, "127.0.0.1", "remote.test", 1)
			}
			conn, err := dialer.Dial("tcp", target)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			conn.SetDeadline(time.Now().Add(2 * time.Second))
			io.WriteString(conn, "GET / HTTP/1.0\r\nHost: test\r\n\r\n")
			body, err := io.ReadAll(conn)
			if err != nil || !strings.Contains(string(body), "via SOCKS") {
				t.Fatalf("SOCKS response = %q, %v", body, err)
			}
			if mode == "direct" && server.Forwarded() != 0 {
				t.Fatal("direct SOCKS request used SSH")
			}
			if mode == "proxy" && server.Forwarded() != 1 {
				t.Fatal("SOCKS request bypassed remote DNS/SSH")
			}
		})
	}
}

func TestSOCKSRejectsUnsupportedAuthentication(t *testing.T) {
	for _, methods := range [][]byte{{0}, {1, 2}, {2, 1, 2}} {
		t.Run(strconv.Itoa(int(methods[0])), func(t *testing.T) {
			addr, _ := testSOCKSProxy(t, "")
			conn, err := net.Dial("tcp", addr)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			conn.SetDeadline(time.Now().Add(time.Second))
			conn.Write(append([]byte{5}, methods...))
			reply := make([]byte, 2)
			if _, err := io.ReadFull(conn, reply); err != nil {
				t.Fatal(err)
			}
			if reply[0] != 5 || reply[1] != 255 {
				t.Fatalf("unsupported auth negotiation = %v, want [5 255]", reply)
			}
		})
	}
}

func TestSOCKSRejectsBlockedDestination(t *testing.T) {
	addr, server := testSOCKSProxy(t, "rules:\n  - DOMAIN,blocked.test,REJECT\n")
	dialer, _ := xproxy.SOCKS5("tcp", addr, nil, &net.Dialer{Timeout: time.Second})
	if conn, err := dialer.Dial("tcp", "blocked.test:80"); err == nil {
		conn.Close()
		t.Fatal("REJECT destination was accepted")
	}
	if server.Forwarded() != 0 {
		t.Fatal("REJECT destination was sent to SSH server")
	}
}

func TestSOCKSRequestValidation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		request []byte
		want    byte
	}{
		{"bind command", []byte{5, 2, 0, 1}, 7},
		{"unknown address type", []byte{5, 1, 0, 9}, 8},
		{"wrong request version", []byte{4, 1, 0, 1, 127, 0, 0, 1, 0, 80}, 1},
		{"nonzero reserved byte", []byte{5, 1, 1, 1, 127, 0, 0, 1, 0, 80}, 1},
		{"empty domain", []byte{5, 1, 0, 3, 0, 0, 80}, 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			addr, _ := testSOCKSProxy(t, "")
			conn, err := net.Dial("tcp", addr)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			conn.SetDeadline(time.Now().Add(time.Second))
			conn.Write([]byte{5, 1, 0})
			greeting := make([]byte, 2)
			if _, err := io.ReadFull(conn, greeting); err != nil {
				t.Fatal(err)
			}
			conn.Write(tc.request)
			reply := make([]byte, 10)
			if _, err := io.ReadFull(conn, reply); err != nil {
				t.Fatal(err)
			}
			if reply[0] != 5 || reply[1] != tc.want {
				t.Fatalf("request reply = %v, want code %d", reply, tc.want)
			}
		})
	}
}

func TestSOCKSReadIPv6Request(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	go func() {
		packet := append([]byte{5, 1, 0, 4}, net.ParseIP("2001:db8::1").To16()...)
		packet = binary.BigEndian.AppendUint16(packet, 443)
		client.Write(packet)
	}()
	proxy := &SOCKS5OverSSH{}
	addr, host, err := proxy.readRequest(server)
	if err != nil || addr != "[2001:db8::1]:443" || host != "2001:db8::1" {
		t.Fatalf("IPv6 request = %q, %q, %v", addr, host, err)
	}
}
