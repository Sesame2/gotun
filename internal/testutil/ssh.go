// Package testutil provides local, disposable servers for integration tests.
package testutil

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Sesame2/gotun/internal/config"
	"golang.org/x/crypto/ssh"
)

type SSHServer struct {
	Addr      string
	KeyFile   string
	forwarded atomic.Int64
}

// NewSSHServer accepts password/key authentication and direct-tcpip forwarding.
// remote.test resolves only here, allowing tests to verify remote DNS behavior.
func NewSSHServer(t testing.TB) *SSHServer {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	s := &SSHServer{KeyFile: filepath.Join(t.TempDir(), "id_ed25519")}
	if err := os.WriteFile(s.KeyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			if c.User() == "tester" && string(password) == "test-password" {
				return nil, nil
			}
			return nil, fmt.Errorf("invalid credentials")
		},
		PublicKeyCallback: func(c ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if c.User() == "tester" && bytes.Equal(key.Marshal(), signer.PublicKey().Marshal()) {
				return nil, nil
			}
			return nil, fmt.Errorf("invalid public key")
		},
	}
	cfg.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s.Addr = listener.Addr().String()
	var mu sync.Mutex
	connections := make(map[net.Conn]bool)
	closing := false
	register := func(conn net.Conn) bool {
		mu.Lock()
		defer mu.Unlock()
		if closing {
			conn.Close()
			return false
		}
		connections[conn] = true
		return true
	}
	unregister := func(conn net.Conn) {
		mu.Lock()
		delete(connections, conn)
		mu.Unlock()
	}
	var workers sync.WaitGroup
	acceptDone := make(chan struct{})
	go func() {
		defer close(acceptDone)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			if !register(conn) {
				continue
			}
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer conn.Close()
				defer unregister(conn)
				server, channels, requests, err := ssh.NewServerConn(conn, cfg)
				if err != nil {
					return
				}
				defer server.Close()
				go ssh.DiscardRequests(requests)
				for incoming := range channels {
					if incoming.ChannelType() != "direct-tcpip" {
						incoming.Reject(ssh.UnknownChannelType, "forwarding only")
						continue
					}
					var request struct {
						Host       string
						Port       uint32
						OriginHost string
						OriginPort uint32
					}
					if err := ssh.Unmarshal(incoming.ExtraData(), &request); err != nil {
						incoming.Reject(ssh.Prohibited, "bad forwarding request")
						continue
					}
					s.forwarded.Add(1)
					if request.Host == "remote.test" || request.Host == "localhost" {
						request.Host = "127.0.0.1"
					}
					if request.Host != "127.0.0.1" && request.Host != "::1" {
						incoming.Reject(ssh.Prohibited, "test server only forwards to loopback")
						continue
					}
					destination, err := net.DialTimeout("tcp", net.JoinHostPort(request.Host, strconv.Itoa(int(request.Port))), time.Second)
					if err != nil {
						incoming.Reject(ssh.ConnectionFailed, err.Error())
						continue
					}
					if !register(destination) {
						incoming.Reject(ssh.Prohibited, "test server is closing")
						continue
					}
					channel, requests, err := incoming.Accept()
					if err != nil {
						unregister(destination)
						destination.Close()
						continue
					}
					go ssh.DiscardRequests(requests)
					workers.Add(1)
					go func() {
						defer workers.Done()
						defer channel.Close()
						defer destination.Close()
						defer unregister(destination)
						done := make(chan struct{})
						go func() {
							io.Copy(destination, channel)
							if tcp, ok := destination.(*net.TCPConn); ok {
								tcp.CloseWrite()
							}
							close(done)
						}()
						io.Copy(channel, destination)
						channel.Close()
						destination.Close()
						<-done
					}()
				}
			}()
		}
	}()
	t.Cleanup(func() {
		mu.Lock()
		closing = true
		mu.Unlock()
		listener.Close()
		<-acceptDone
		mu.Lock()
		for conn := range connections {
			conn.Close()
		}
		mu.Unlock()
		workers.Wait()
	})
	return s
}

func (s *SSHServer) Config() *config.Config {
	cfg := config.NewConfig()
	cfg.SSHServer = s.Addr
	cfg.SSHUser = "tester"
	cfg.SSHPassword = "test-password"
	cfg.SSHKeyFile = s.KeyFile
	cfg.InteractiveAuth = false
	cfg.SystemProxy = false
	cfg.ListenAddr = "127.0.0.1:0"
	cfg.Timeout = time.Second
	return cfg
}

func (s *SSHServer) Forwarded() int64 { return s.forwarded.Load() }

// AvailableAddr reserves an ephemeral port only long enough to discover it.
func AvailableAddr(t testing.TB) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	return addr
}

func WaitTCP(t testing.TB, addr string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("listener %s did not become ready", addr)
}
