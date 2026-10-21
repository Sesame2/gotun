package proxy

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/Sesame2/gotun/internal/config"
	"github.com/Sesame2/gotun/internal/logger"
	"github.com/Sesame2/gotun/internal/router"
	"github.com/Sesame2/gotun/internal/testutil"
	xproxy "golang.org/x/net/proxy"
)

func tcpTarget(t *testing.T, handle func(net.Conn)) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	connections := make(map[net.Conn]bool)
	var workers sync.WaitGroup
	accepted := make(chan struct{})
	go func() {
		defer close(accepted)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			connections[conn] = true
			mu.Unlock()
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer conn.Close()
				defer func() { mu.Lock(); delete(connections, conn); mu.Unlock() }()
				handle(conn)
			}()
		}
	}()
	t.Cleanup(func() {
		listener.Close()
		<-accepted
		mu.Lock()
		for conn := range connections {
			conn.Close()
		}
		mu.Unlock()
		workers.Wait()
	})
	return listener.Addr().String()
}

func lifecycleProxy(t *testing.T, protocol, mode string) (string, func() error) {
	t.Helper()
	cfg := config.NewConfig()
	cfg.ListenAddr = "127.0.0.1:0"
	cfg.SocksAddr = "127.0.0.1:0"
	log := logger.NewLogger(false)
	var client *SSHClient
	var rules *router.Router
	if mode == "proxy" {
		server := testutil.NewSSHServer(t)
		cfg = server.Config()
		cfg.SocksAddr = "127.0.0.1:0"
		var err error
		client, err = NewSSHClient(cfg, log)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { client.Close() })
	} else {
		rules = testRouter(t, "mode: direct\n")
	}
	var addr string
	var closeProxy func() error
	var start func() error
	if protocol == "HTTP" {
		p, err := NewHTTPOverSSH(cfg, log, client, rules)
		if err != nil {
			t.Fatal(err)
		}
		addr, start, closeProxy = p.listener.Addr().String(), p.Start, p.Close
	} else {
		p, err := NewSOCKS5OverSSH(cfg, log, client, rules)
		if err != nil {
			t.Fatal(err)
		}
		addr, start, closeProxy = p.listener.Addr().String(), p.Start, p.Close
	}
	done := make(chan error, 1)
	go func() { done <- start() }()
	t.Cleanup(func() {
		closeProxy()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("proxy server stopped with: %v", err)
			}
		case <-time.After(3 * time.Second):
			t.Error("proxy listener did not stop")
		}
	})
	return addr, closeProxy
}

func openTunnel(t *testing.T, protocol, addr, target string) net.Conn {
	t.Helper()
	var conn net.Conn
	var err error
	if protocol == "HTTP" {
		conn, err = net.DialTimeout("tcp", addr, time.Second)
		if err == nil {
			conn.SetDeadline(time.Now().Add(2 * time.Second))
			fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target)
			response, readErr := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: http.MethodConnect})
			if readErr != nil || response.StatusCode != http.StatusOK {
				conn.Close()
				t.Fatalf("CONNECT failed: %v, %v", response, readErr)
			}
		}
	} else {
		dialer, dialErr := xproxy.SOCKS5("tcp", addr, nil, &net.Dialer{Timeout: time.Second})
		if dialErr != nil {
			t.Fatal(dialErr)
		}
		conn, err = dialer.Dial("tcp", target)
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	conn.SetDeadline(time.Now().Add(2 * time.Second))
	return conn
}

func TestProxyCloseDisconnectsActiveTunnels(t *testing.T) {
	for _, protocol := range []string{"HTTP", "SOCKS5"} {
		for _, mode := range []string{"direct", "proxy"} {
			t.Run(protocol+"/"+mode, func(t *testing.T) {
				target := tcpTarget(t, func(conn net.Conn) { io.Copy(conn, conn) })
				addr, closeProxy := lifecycleProxy(t, protocol, mode)
				conn := openTunnel(t, protocol, addr, target)
				io.WriteString(conn, "before")
				buf := make([]byte, len("before"))
				if _, err := io.ReadFull(conn, buf); err != nil || string(buf) != "before" {
					t.Fatalf("initial echo = %q, %v", buf, err)
				}
				if err := closeProxy(); err != nil {
					t.Fatal(err)
				}
				conn.SetReadDeadline(time.Now().Add(time.Second))
				if _, err := conn.Read(buf); err == nil {
					t.Fatal("closed proxy left its existing tunnel open")
				} else if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
					t.Fatal("closed proxy did not disconnect its existing tunnel")
				}
			})
		}
	}
}

func TestProxyTunnelPreservesResponseAfterClientCloseWrite(t *testing.T) {
	for _, protocol := range []string{"HTTP", "SOCKS5"} {
		for _, mode := range []string{"direct", "proxy"} {
			t.Run(protocol+"/"+mode, func(t *testing.T) {
				target := tcpTarget(t, func(conn net.Conn) {
					payload, err := io.ReadAll(conn)
					if err == nil {
						fmt.Fprintf(conn, "response:%s", payload)
					}
				})
				addr, _ := lifecycleProxy(t, protocol, mode)
				conn := openTunnel(t, protocol, addr, target)
				if _, err := io.WriteString(conn, "request"); err != nil {
					t.Fatal(err)
				}
				if err := conn.(*net.TCPConn).CloseWrite(); err != nil {
					t.Fatal(err)
				}
				response, err := io.ReadAll(conn)
				if err != nil || string(response) != "response:request" {
					t.Fatalf("response after half-close = %q, %v", response, err)
				}
			})
		}
	}
}

func TestSOCKSCloseDisconnectsPendingRequest(t *testing.T) {
	addr, closeProxy := lifecycleProxy(t, "SOCKS5", "direct")
	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(time.Second))
	conn.Write([]byte{5, 1, 0})
	if _, err := io.ReadFull(conn, make([]byte, 2)); err != nil {
		t.Fatal(err)
	}
	// The server is now blocked reading the next handshake/request bytes.
	if err := closeProxy(); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Read(make([]byte, 1)); err == nil {
		t.Fatal("closed SOCKS proxy left pending request open")
	} else if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatal("closed SOCKS proxy did not disconnect pending request")
	}
}

func TestSSHFixtureCleanupClosesStalledDestination(t *testing.T) {
	var once sync.Once
	release := make(chan struct{})
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	target := tcpTarget(t, func(conn net.Conn) { <-release })
	timedOut := make(chan struct{})
	watchdog := time.AfterFunc(3*time.Second, func() { close(timedOut); unblock() })
	defer watchdog.Stop()
	t.Run("fixture", func(t *testing.T) {
		server := testutil.NewSSHServer(t)
		client, err := NewSSHClient(server.Config(), logger.NewLogger(false))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { client.Close() })
		conn, err := client.Dial("tcp", target)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.Close() })
	})
	select {
	case <-timedOut:
		t.Fatal("SSH fixture cleanup waited for the stalled target to be released")
	default:
	}
}

func TestProxyCloseWhileConnectionsArrive(t *testing.T) {
	for _, protocol := range []string{"HTTP", "SOCKS5"} {
		for _, mode := range []string{"direct", "proxy"} {
			t.Run(protocol+"/"+mode, func(t *testing.T) {
				target := tcpTarget(t, func(conn net.Conn) { io.Copy(conn, conn) })
				addr, closeProxy := lifecycleProxy(t, protocol, mode)
				var clients []net.Conn
				for i := 0; i < 12; i++ {
					conn, err := net.DialTimeout("tcp", addr, time.Second)
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { conn.Close() })
					clients = append(clients, conn)
				}
				start := make(chan struct{})
				var workers sync.WaitGroup
				for _, conn := range clients {
					workers.Add(1)
					go func(conn net.Conn) {
						defer workers.Done()
						<-start
						conn.SetDeadline(time.Now().Add(2 * time.Second))
						if protocol == "HTTP" {
							fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target)
						} else {
							_, portText, _ := net.SplitHostPort(target)
							port, _ := strconv.Atoi(portText)
							request := []byte{5, 1, 0, 5, 1, 0, 1, 127, 0, 0, 1}
							conn.Write(binary.BigEndian.AppendUint16(request, uint16(port)))
						}
						if _, err := io.ReadAll(conn); err != nil {
							if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
								t.Errorf("connection arriving during close was left open: %v", err)
							}
						}
					}(conn)
				}
				close(start)
				if err := closeProxy(); err != nil {
					t.Fatal(err)
				}
				workers.Wait()
			})
		}
	}
}
