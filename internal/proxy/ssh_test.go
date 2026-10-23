package proxy_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Sesame2/gotun/internal/config"
	"github.com/Sesame2/gotun/internal/logger"
	"github.com/Sesame2/gotun/internal/proxy"
	"github.com/Sesame2/gotun/internal/testutil"
	"golang.org/x/crypto/ssh/knownhosts"
)

func TestSSHRejectsUnknownHostKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	server := testutil.NewSSHServer(t)
	cfg := server.Config()
	cfg.KnownHostsFile = ""
	client, err := proxy.NewSSHClient(cfg, logger.NewLogger(false))
	if client != nil {
		client.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "known_hosts") {
		t.Fatalf("unknown SSH host key must fail with trust-file guidance, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".ssh", "known_hosts")); !os.IsNotExist(err) {
		t.Fatalf("unknown host was persisted without consent: %v", err)
	}
}

func TestSSHUnknownHostRequiresConsentAndPersistsTrust(t *testing.T) {
	server := testutil.NewSSHServer(t)
	cfg := server.Config()
	cfg.KnownHostsFile = filepath.Join(t.TempDir(), ".ssh", "known_hosts")
	prompts := 0
	cfg.HostKeyPrompt = func(host, fingerprint string) bool {
		prompts++
		if host != server.Addr || !strings.HasPrefix(fingerprint, "SHA256:") {
			t.Errorf("unverifiable prompt metadata: %q %q", host, fingerprint)
		}
		return false
	}
	if c, err := proxy.NewSSHClient(cfg, logger.NewLogger(false)); err == nil {
		c.Close()
		t.Fatal("declined host key was accepted")
	}
	if _, err := os.Stat(cfg.KnownHostsFile); !os.IsNotExist(err) {
		t.Fatalf("declined key changed trust file: %v", err)
	}
	cfg.HostKeyPrompt = func(string, string) bool { prompts++; return true }
	client, err := proxy.NewSSHClient(cfg, logger.NewLogger(false))
	if err != nil {
		t.Fatalf("explicitly accepted key failed: %v", err)
	}
	client.Close()
	cfg.HostKeyPrompt = nil
	client, err = proxy.NewSSHClient(cfg, logger.NewLogger(false))
	if err != nil {
		t.Fatalf("accepted key was not persisted: %v", err)
	}
	client.Close()
	if prompts != 2 {
		t.Fatalf("confirmation count = %d, want decline then accept", prompts)
	}
}

func TestSSHConfirmationWaitDoesNotUseHandshakeTimeout(t *testing.T) {
	server := testutil.NewSSHServer(t)
	cfg := server.Config()
	cfg.KnownHostsFile = filepath.Join(t.TempDir(), "known_hosts")
	cfg.Timeout = 200 * time.Millisecond
	opened, release, promptDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	cfg.HostKeyPrompt = func(string, string) bool {
		close(opened)
		<-release
		close(promptDone)
		return true
	}
	done := make(chan error, 1)
	go func() {
		c, err := proxy.NewSSHClient(cfg, logger.NewLogger(false))
		if c != nil {
			c.Close()
		}
		done <- err
	}()
	<-opened
	select {
	case err := <-done:
		close(release)
		<-promptDone
		t.Fatalf("human confirmation consumed handshake timeout: %v", err)
	case <-time.After(350 * time.Millisecond):
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("accepted key did not reconnect: %v", err)
	}
}

func TestSSHCancelledConfirmationCannotPersistLateAcceptance(t *testing.T) {
	server := testutil.NewSSHServer(t)
	cfg := server.Config()
	cfg.KnownHostsFile = filepath.Join(t.TempDir(), "known_hosts")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg.Context = ctx
	opened, release, promptDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	cfg.HostKeyPrompt = func(string, string) bool { close(opened); <-release; close(promptDone); return true }
	done := make(chan error, 1)
	go func() {
		c, err := proxy.NewSSHClient(cfg, logger.NewLogger(false))
		if c != nil {
			c.Close()
		}
		done <- err
	}()
	<-opened
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("confirmation cancellation = %v", err)
		}
	case <-time.After(time.Second):
		t.Error("cancelled host-key confirmation did not return")
	}
	close(release)
	<-promptDone
	if _, err := os.Stat(cfg.KnownHostsFile); !os.IsNotExist(err) {
		t.Fatalf("late confirmation changed trust file: %v", err)
	}
}

func TestSSHReconnectRejectsKeyChangedDuringConfirmation(t *testing.T) {
	first, second := testutil.NewSSHServer(t), testutil.NewSSHServer(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	var workers sync.WaitGroup
	workers.Add(1)
	go func() {
		defer workers.Done()
		for _, address := range []string{first.Addr, second.Addr} {
			client, err := listener.Accept()
			if err != nil {
				return
			}
			upstream, err := net.Dial("tcp", address)
			if err != nil {
				client.Close()
				return
			}
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer client.Close()
				defer upstream.Close()
				done := make(chan struct{})
				go func() { io.Copy(upstream, client); upstream.Close(); close(done) }()
				io.Copy(client, upstream)
				client.Close()
				<-done
			}()
		}
	}()
	cfg := first.Config()
	cfg.SSHServer = listener.Addr().String()
	cfg.KnownHostsFile = filepath.Join(t.TempDir(), "known_hosts")
	prompts := 0
	cfg.HostKeyPrompt = func(string, string) bool { prompts++; return true }
	client, err := proxy.NewSSHClient(cfg, logger.NewLogger(false))
	if client != nil {
		client.Close()
	}
	listener.Close()
	workers.Wait()
	if err == nil || prompts != 1 || !strings.Contains(err.Error(), "key mismatch") {
		t.Fatalf("confirmation trusted a different reconnect key: err=%v prompts=%d", err, prompts)
	}
}

func TestSSHDefaultTrustFileAlsoVerifiesJumpHosts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	server := testutil.NewSSHServer(t)
	trust, err := os.ReadFile(server.KnownHostsFile)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".ssh", "known_hosts")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, trust, 0600); err != nil {
		t.Fatal(err)
	}
	cfg := server.Config()
	cfg.KnownHostsFile = ""
	c, err := proxy.NewSSHClient(cfg, logger.NewLogger(false))
	if err != nil {
		t.Fatalf("default known_hosts not used: %v", err)
	}
	c.Close()
	jump := testutil.NewSSHServer(t)
	host, port, _ := net.SplitHostPort(jump.Addr)
	cfg.JumpHostsList = []config.JumpHost{{Host: host, Port: port, User: "tester", Password: "test-password"}}
	c, err = proxy.NewSSHClient(cfg, logger.NewLogger(false))
	if c != nil {
		c.Close()
	}
	if err == nil {
		t.Fatal("unknown jump host bypassed verification")
	}
	jumpTrust, err := os.ReadFile(jump.KnownHostsFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(trust, jumpTrust...), 0600); err != nil {
		t.Fatal(err)
	}
	c, err = proxy.NewSSHClient(cfg, logger.NewLogger(false))
	if err != nil {
		t.Fatalf("trusted jump failed: %v", err)
	}
	defer c.Close()
	if c.RemoteAddr().String() != jump.Addr {
		t.Fatalf("physical peer = %s, want jump %s", c.RemoteAddr(), jump.Addr)
	}
}

func TestSSHChangedRevokedOrInvalidTrustCannotBeAccepted(t *testing.T) {
	server := testutil.NewSSHServer(t)
	other := testutil.NewSSHServer(t)
	otherTrust, err := os.ReadFile(other.KnownHostsFile)
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(string(otherTrust))
	wrong := knownhosts.Normalize(server.Addr) + " " + fields[1] + " " + fields[2] + "\n"
	actual, err := os.ReadFile(server.KnownHostsFile)
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"changed": wrong, "revoked": "@revoked " + string(actual), "invalid": "broken trust file\n"} {
		t.Run(name, func(t *testing.T) {
			cfg := server.Config()
			cfg.KnownHostsFile = filepath.Join(t.TempDir(), "known_hosts")
			if err := os.WriteFile(cfg.KnownHostsFile, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			prompts := 0
			cfg.HostKeyPrompt = func(string, string) bool { prompts++; return true }
			client, err := proxy.NewSSHClient(cfg, logger.NewLogger(false))
			if client != nil {
				client.Close()
			}
			if err == nil || prompts != 0 {
				t.Fatalf("unsafe trust was bypassed: err=%v prompts=%d", err, prompts)
			}
			got, err := os.ReadFile(cfg.KnownHostsFile)
			if err != nil || string(got) != content {
				t.Fatalf("rejected key modified trust: %q %v", got, err)
			}
		})
	}
}

func TestSSHSilentHandshakeTimesOutOrCancels(t *testing.T) {
	for _, throughJump := range []bool{false, true} {
		for _, cancelStart := range []bool{false, true} {
			name := fmt.Sprintf("jump=%v/cancel=%v", throughJump, cancelStart)
			t.Run(name, func(t *testing.T) {
				listener, err := net.Listen("tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				defer listener.Close()
				accepted := make(chan net.Conn, 1)
				go func() {
					c, err := listener.Accept()
					if err == nil {
						accepted <- c
					}
				}()
				cfg := config.NewConfig()
				cfg.SSHServer, cfg.SSHUser, cfg.SSHPassword = listener.Addr().String(), "tester", "synthetic"
				cfg.InteractiveAuth = false
				cfg.SSHKeyFile = filepath.Join(t.TempDir(), "missing-key")
				cfg.KnownHostsFile = filepath.Join(t.TempDir(), "known_hosts")
				if throughJump {
					jump := testutil.NewSSHServer(t)
					cfg = jump.Config()
					cfg.SSHPassword = ""
					cfg.SSHServer = listener.Addr().String()
					host, port, _ := net.SplitHostPort(jump.Addr)
					cfg.JumpHostsList = []config.JumpHost{{Host: host, Port: port, User: "tester", Password: "test-password"}}
				}
				cfg.Timeout = 300 * time.Millisecond
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				cfg.Context = ctx
				if cancelStart {
					cfg.Timeout = 10 * time.Second
				}
				done := make(chan error, 1)
				go func() {
					client, err := proxy.NewSSHClient(cfg, logger.NewLogger(false))
					if client != nil {
						client.Close()
					}
					done <- err
				}()
				var conn net.Conn
				select {
				case conn = <-accepted:
				case <-time.After(2 * time.Second):
					t.Fatal("silent server was not reached")
				}
				defer conn.Close()
				if cancelStart {
					cancel()
				}
				select {
				case err := <-done:
					want := context.DeadlineExceeded
					if cancelStart {
						want = context.Canceled
					}
					if !errors.Is(err, want) {
						t.Fatalf("SSH cancellation cause = %v, want %v", err, want)
					}
				case <-time.After(time.Second):
					conn.Close()
					t.Fatal("SSH handshake ignored timeout/cancellation")
				}
			})
		}
	}
}

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
			if jump != nil {
				trust, err := os.ReadFile(jump.KnownHostsFile)
				if err != nil {
					t.Fatal(err)
				}
				file, err := os.OpenFile(cfg.KnownHostsFile, os.O_APPEND|os.O_WRONLY, 0600)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := file.Write(trust); err != nil {
					file.Close()
					t.Fatal(err)
				}
				if err := file.Close(); err != nil {
					t.Fatal(err)
				}
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
