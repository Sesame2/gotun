package proxy_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Sesame2/gotun/internal/config"
	"github.com/Sesame2/gotun/internal/logger"
	"github.com/Sesame2/gotun/internal/proxy"
	"github.com/Sesame2/gotun/internal/testutil"
)

func TestSSHAuthenticationAndRemoteForwarding(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("forwarded through SSH"))
	}))
	defer origin.Close()
	for _, auth := range []string{"password", "key", "structured jump", "legacy jump"} {
		t.Run(auth, func(t *testing.T) {
			server := testutil.NewSSHServer(t)
			cfg := server.Config()
			var jump *testutil.SSHServer
			switch auth {
			case "password":
				cfg.SSHKeyFile = filepath.Join(t.TempDir(), "missing-key")
			case "key":
				cfg.SSHPassword = ""
			case "structured jump":
				jump = testutil.NewSSHServer(t)
				cfg.JumpHostsList = []config.JumpHost{{Host: strings.Split(jump.Addr, ":")[0], Port: strings.Split(jump.Addr, ":")[1], User: "tester", Password: "test-password"}}
			case "legacy jump":
				jump = testutil.NewSSHServer(t)
				cfg.JumpHosts = []string{"tester@" + jump.Addr}
			}
			client, err := proxy.NewSSHClient(cfg, logger.NewLogger(false))
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			remoteAddr := strings.Replace(strings.TrimPrefix(origin.URL, "http://"), "127.0.0.1", "remote.test", 1)
			conn, err := client.Dial("tcp", remoteAddr)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			if _, err := io.WriteString(conn, "GET / HTTP/1.0\r\nHost: remote.test\r\n\r\n"); err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(conn)
			if err != nil || !strings.Contains(string(body), "forwarded through SSH") {
				t.Fatalf("SSH forwarding = %q, %v", body, err)
			}
			if server.Forwarded() != 1 {
				t.Fatalf("forward count = %d", server.Forwarded())
			}
			if jump != nil && jump.Forwarded() != 1 {
				t.Fatalf("jump server forwarded %d connections, want 1", jump.Forwarded())
			}
			if err := client.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSSHRejectsInvalidCredentialsAndMissingKey(t *testing.T) {
	server := testutil.NewSSHServer(t)
	cfg := server.Config()
	cfg.SSHPassword = "wrong-password"
	cfg.SSHKeyFile = filepath.Join(t.TempDir(), "missing-key")
	if client, err := proxy.NewSSHClient(cfg, logger.NewLogger(false)); err == nil {
		client.Close()
		t.Fatal("invalid credentials were accepted")
	}
	cfg.SSHPassword = ""
	if client, err := proxy.NewSSHClient(cfg, logger.NewLogger(false)); err == nil {
		client.Close()
		t.Fatal("missing credentials were accepted")
	}
}

func TestUnconnectedSSHDialReturnsError(t *testing.T) {
	var client proxy.SSHClient
	if conn, err := client.Dial("tcp", "127.0.0.1:80"); err == nil || conn != nil {
		t.Fatalf("unconnected Dial = %v, %v", conn, err)
	}
}
