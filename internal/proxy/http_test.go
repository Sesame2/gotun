package proxy

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Sesame2/gotun/internal/logger"
	"github.com/Sesame2/gotun/internal/router"
	"github.com/Sesame2/gotun/internal/testutil"
)

func testRouter(t *testing.T, rules string) *router.Router {
	t.Helper()
	file := filepath.Join(t.TempDir(), "rules.yaml")
	if err := os.WriteFile(file, []byte(rules), 0600); err != nil {
		t.Fatal(err)
	}
	r, err := router.NewRouter(file)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func testHTTPProxy(t *testing.T, rules string) (*httptest.Server, *testutil.SSHServer) {
	t.Helper()
	sshServer := testutil.NewSSHServer(t)
	cfg := sshServer.Config()
	log := logger.NewLogger(false)
	client, err := NewSSHClient(cfg, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	var r *router.Router
	if rules != "" {
		r = testRouter(t, rules)
	}
	p, err := NewHTTPOverSSH(cfg, log, client, r)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Close() })
	server := httptest.NewServer(http.HandlerFunc(p.handleHTTP))
	t.Cleanup(server.Close)
	return server, sshServer
}

func proxyHTTPClient(t *testing.T, proxyURL string) *http.Client {
	t.Helper()
	u, err := url.Parse(proxyURL)
	if err != nil {
		t.Fatal(err)
	}
	transport := &http.Transport{Proxy: http.ProxyURL(u)}
	t.Cleanup(transport.CloseIdleConnections)
	return &http.Client{Transport: transport, Timeout: 3 * time.Second}
}

func TestHTTPForwarding(t *testing.T) {
	for _, mode := range []string{"proxy", "direct"} {
		t.Run(mode, func(t *testing.T) {
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
				w.Header().Set("X-Origin", "fixture")
				w.WriteHeader(http.StatusCreated)
				fmt.Fprintf(w, "%s %s %s %s", r.Method, r.URL.RequestURI(), r.Header.Get("X-Test"), body)
			}))
			defer origin.Close()
			rules := ""
			if mode == "direct" {
				rules = "mode: direct\n"
			}
			p, sshServer := testHTTPProxy(t, rules)
			target := origin.URL + "/path?q=1"
			if mode == "proxy" {
				target = strings.Replace(target, "127.0.0.1", "remote.test", 1)
			}
			req, _ := http.NewRequest(http.MethodPost, target, strings.NewReader("payload"))
			req.Header.Set("X-Test", "kept")
			resp, err := proxyHTTPClient(t, p.URL).Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != http.StatusCreated || string(body) != "POST /path?q=1 kept payload" || resp.Header.Get("X-Origin") != "fixture" {
				t.Fatalf("response = %d %q, headers=%v", resp.StatusCode, body, resp.Header)
			}
			if mode == "direct" && sshServer.Forwarded() != 0 {
				t.Fatal("direct request used SSH")
			}
			if mode != "direct" && sshServer.Forwarded() != 1 {
				t.Fatal("proxied request bypassed SSH")
			}
		})
	}
}

func TestHTTPRejectRuleBlocksBothHTTPAndCONNECT(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodConnect} {
		t.Run(method, func(t *testing.T) {
			p, server := testHTTPProxy(t, "rules:\n  - DOMAIN,blocked.test,REJECT\n")
			req := httptest.NewRequest(method, "http://blocked.test:80/", nil)
			if method == http.MethodConnect {
				req.Host = "blocked.test:443"
			}
			w := httptest.NewRecorder()
			p.Config.Handler.ServeHTTP(w, req)
			if w.Code != http.StatusForbidden {
				t.Fatalf("rejected request status = %d, want 403", w.Code)
			}
			if server.Forwarded() != 0 {
				t.Fatal("REJECT rule attempted a connection")
			}
		})
	}
}

func TestHTTPRelativeURLRejected(t *testing.T) {
	p, _ := testHTTPProxy(t, "")
	w := httptest.NewRecorder()
	p.Config.Handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/relative", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("relative request = %d, want 400", w.Code)
	}
}

func TestHTTPDoesNotLeakProxyCredentialsOrHopHeaders(t *testing.T) {
	for _, mode := range []string{"proxy", "direct"} {
		t.Run(mode, func(t *testing.T) {
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Proxy-Authorization") != "" || r.Header.Get("X-Private") != "" {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				io.WriteString(w, "clean headers")
			}))
			defer origin.Close()
			rules := ""
			if mode == "direct" {
				rules = "mode: direct\n"
			}
			p, _ := testHTTPProxy(t, rules)
			req, _ := http.NewRequest(http.MethodGet, origin.URL, nil)
			req.Header.Set("Proxy-Authorization", "Basic c2VjcmV0OnNlY3JldA==")
			req.Header.Set("Connection", "X-Private")
			req.Header.Set("X-Private", "hop-only")
			resp, err := proxyHTTPClient(t, p.URL).Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != 200 {
				t.Fatalf("proxy leaked connection headers/credentials to origin: status=%d", resp.StatusCode)
			}
		})
	}
}

func TestHTTPForcedUpstreamAndUnreachableOrigin(t *testing.T) {
	server := testutil.NewSSHServer(t)
	cfg := server.Config()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, r.Host) }))
	defer origin.Close()
	cfg.HTTPUpstream = strings.TrimPrefix(origin.URL, "http://")
	sshClient, err := NewSSHClient(cfg, logger.NewLogger(false))
	if err != nil {
		t.Fatal(err)
	}
	defer sshClient.Close()
	p, err := NewHTTPOverSSH(cfg, logger.NewLogger(false), sshClient, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	proxy := httptest.NewServer(http.HandlerFunc(p.handleHTTP))
	defer proxy.Close()
	client := proxyHTTPClient(t, proxy.URL)
	resp, err := client.Get("http://unresolvable.test/path")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || string(body) != "unresolvable.test" {
		t.Fatalf("upstream response=%d %q", resp.StatusCode, body)
	}
	// Stop the real origin: the same public proxy must now return 502.
	origin.Close()
	resp, err = client.Get("http://unresolvable.test/path")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("unreachable origin status = %d", resp.StatusCode)
	}
}

func TestHTTPConnectPreservesBufferedPayload(t *testing.T) {
	for _, mode := range []string{"proxy", "direct"} {
		t.Run(mode, func(t *testing.T) {
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "tunneled") }))
			defer origin.Close()
			rules := ""
			if mode == "direct" {
				rules = "mode: direct\n"
			}
			p, _ := testHTTPProxy(t, rules)
			conn, err := net.Dial("tcp", strings.TrimPrefix(p.URL, "http://"))
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			conn.SetDeadline(time.Now().Add(2 * time.Second))
			target := strings.TrimPrefix(origin.URL, "http://")
			fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\nGET / HTTP/1.0\r\nHost: fixture\r\n\r\n", target, target)
			reader := bufio.NewReader(conn)
			connect, err := http.ReadResponse(reader, &http.Request{Method: http.MethodConnect})
			if err != nil || connect.StatusCode != 200 {
				t.Fatalf("CONNECT = %v, %v", connect, err)
			}
			resp, err := http.ReadResponse(reader, &http.Request{Method: http.MethodGet})
			if err != nil {
				t.Fatalf("buffered tunnel payload was lost: %v", err)
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			if string(body) != "tunneled" {
				t.Fatalf("tunnel response = %q", body)
			}
		})
	}
}
