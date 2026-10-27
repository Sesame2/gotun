package service_test

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	stdlog "log"
	"mime"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Sesame2/gotun/internal/config"
	"github.com/Sesame2/gotun/internal/logger"
	"github.com/Sesame2/gotun/internal/proxy"
	"github.com/Sesame2/gotun/internal/service"
	"github.com/Sesame2/gotun/internal/testutil"
	"golang.org/x/net/html"
)

type liveHTTPSOptions struct {
	configPath, profileID, requestURL, expectedTitle string
}

type liveHTTPSResult struct {
	host   string
	status int
}

// TestLiveCompanyHTTPS is opt-in. It never changes the saved configuration,
// system proxy, or routes. The URL and response body must not enter test logs.
func TestLiveCompanyHTTPS(t *testing.T) {
	options, enabled, err := liveOptions(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	if !enabled {
		t.Skip("set GOTUN_LIVE_CONFIG, GOTUN_LIVE_PROFILE and GOTUN_LIVE_URL to enable")
	}
	profile, err := readLiveProfile(options.configPath, options.profileID)
	if err != nil {
		t.Fatal(err)
	}
	result, err := checkProfileHTTPS(profile, options.requestURL, options.expectedTitle, nil)
	if err != nil {
		t.Fatalf("live HTTPS check failed: %v", err)
	}
	t.Logf("HTTPS host=%q status=%d", result.host, result.status)
}

func liveOptions(getenv func(string) string) (liveHTTPSOptions, bool, error) {
	options := liveHTTPSOptions{
		configPath: getenv("GOTUN_LIVE_CONFIG"), profileID: getenv("GOTUN_LIVE_PROFILE"),
		requestURL: getenv("GOTUN_LIVE_URL"), expectedTitle: getenv("GOTUN_LIVE_EXPECTED_TITLE"),
	}
	if options.configPath == "" && options.profileID == "" && options.requestURL == "" {
		return options, false, nil
	}
	if options.configPath == "" || options.profileID == "" || options.requestURL == "" {
		return options, true, errors.New("live integration requires GOTUN_LIVE_CONFIG, GOTUN_LIVE_PROFILE and GOTUN_LIVE_URL together")
	}
	if options.expectedTitle == "" {
		options.expectedTitle = "IDE"
	}
	if _, err := liveHTTPSURL(options.requestURL); err != nil {
		return options, true, err
	}
	return options, true, nil
}

func liveHTTPSURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
		return nil, errors.New("live target must be an absolute HTTPS URL without embedded credentials")
	}
	return u, nil
}

func liveHTTPSOrigin(u *url.URL) string {
	port := u.Port()
	if port == "" {
		port = "443"
	}
	return net.JoinHostPort(strings.ToLower(u.Hostname()), port)
}

func TestLiveHTTPSOriginNormalizesHostnameAndPort(t *testing.T) {
	for _, tc := range []struct{ target, want string }{
		{"https://EXAMPLE.test/path", "example.test:443"},
		{"https://example.test:443/path", "example.test:443"},
		{"https://example.test:444/path", "example.test:444"},
		{"https://[::1]/path", "[::1]:443"},
	} {
		u, err := liveHTTPSURL(tc.target)
		if err != nil || liveHTTPSOrigin(u) != tc.want {
			t.Fatalf("unexpected normalized HTTPS origin for fixture %q", tc.target)
		}
	}
}

func readLiveProfile(path, id string) (service.SSHProfile, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return service.SSHProfile{}, errors.New("cannot read live configuration")
	}
	var data service.ConfigData
	if err := json.Unmarshal(contents, &data); err != nil {
		return service.SSHProfile{}, errors.New("cannot decode live configuration")
	}
	var matches []service.SSHProfile
	for _, profile := range data.Profiles {
		if profile.ID == id {
			matches = append(matches, profile)
		}
	}
	if len(matches) != 1 {
		return service.SSHProfile{}, errors.New("live profile ID must match exactly one saved profile")
	}
	return matches[0], nil
}

func checkProfileHTTPS(profile service.SSHProfile, rawURL, expectedTitle string, roots *x509.CertPool) (result liveHTTPSResult, err error) {
	target, err := liveHTTPSURL(rawURL)
	if err != nil {
		return result, err
	}
	cfg := (&service.ConfigService{}).ProfileToConfig(&profile)
	cfg.InteractiveAuth = false
	cfg.SystemProxy = false
	cfg.SocksAddr = ""
	if err := cfg.Validate(); err != nil {
		return result, errors.New("live SSH profile validation failed")
	}
	// Suppress production logs: native SSH errors may contain private hosts,
	// while HTTP errors can include a URL's authorization query parameters.
	log := logger.NewLogger(false)
	log.SetLevel(logger.LevelFatal)
	sshClient, err := proxy.NewSSHClient(cfg, log)
	if err != nil {
		return result, errors.New("live SSH connection failed")
	}
	defer sshClient.Close()

	reservation, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return result, errors.New("cannot reserve a loopback proxy port")
	}
	cfg.ListenAddr = reservation.Addr().String()
	if err := reservation.Close(); err != nil {
		return result, errors.New("cannot release loopback proxy port reservation")
	}
	httpProxy, err := proxy.NewHTTPOverSSH(cfg, log, sshClient, nil)
	if err != nil {
		return result, errors.New("cannot create temporary HTTP proxy")
	}
	done := make(chan error, 1)
	go func() { done <- httpProxy.Start() }()
	defer func() {
		closeErr := httpProxy.Close()
		select {
		case serveErr := <-done:
			if err == nil && (closeErr != nil || serveErr != nil) {
				err = errors.New("temporary HTTP proxy did not stop cleanly")
			}
		case <-time.After(3 * time.Second):
			if err == nil {
				err = errors.New("temporary HTTP proxy shutdown timed out")
			}
		}
	}()
	proxyURL := &url.URL{Scheme: "http", Host: cfg.ListenAddr}
	transport := &http.Transport{
		Proxy: http.ProxyURL(proxyURL), TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport: transport, Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			req.Header.Del("Referer")
			if len(via) >= 10 || req.URL.Scheme != "https" || liveHTTPSOrigin(req.URL) != liveHTTPSOrigin(target) {
				return errors.New("live HTTPS redirect rejected")
			}
			return nil
		},
	}
	response, err := client.Get(rawURL)
	if err != nil {
		return result, errors.New("live HTTPS request failed (CONNECT, TLS or redirect)")
	}
	defer response.Body.Close()
	result = liveHTTPSResult{host: response.Request.URL.Hostname(), status: response.StatusCode}
	if response.StatusCode != http.StatusOK {
		return result, errors.New("live HTTPS response status is not 200")
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || (mediaType != "text/html" && mediaType != "application/xhtml+xml") {
		return result, errors.New("live HTTPS response is not HTML")
	}
	const maxHTMLBytes = 2 << 20
	body, err := io.ReadAll(io.LimitReader(response.Body, maxHTMLBytes+1))
	if err != nil || len(body) > maxHTMLBytes {
		return result, errors.New("live HTTPS HTML response could not be read within limit")
	}
	document, err := html.Parse(bytes.NewReader(body))
	if err != nil || htmlTitle(document) != strings.Join(strings.Fields(expectedTitle), " ") {
		return result, errors.New("live HTTPS HTML title does not match expected title")
	}
	return result, nil
}

func htmlTitle(node *html.Node) string {
	if node.Type == html.ElementNode && node.Data == "title" {
		var text strings.Builder
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if child.Type == html.TextNode {
				text.WriteString(child.Data)
			}
		}
		return strings.Join(strings.Fields(text.String()), " ")
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if title := htmlTitle(child); title != "" {
			return title
		}
	}
	return ""
}

func TestLiveIntegrationOptInValidation(t *testing.T) {
	for _, tc := range []struct {
		name, title string
		values      map[string]string
		enabled     bool
		fail        bool
	}{
		{name: "not configured"},
		{name: "partial configuration fails", values: map[string]string{"GOTUN_LIVE_CONFIG": "private-config"}, enabled: true, fail: true},
		{name: "valid defaults", values: map[string]string{"GOTUN_LIVE_CONFIG": "config.json", "GOTUN_LIVE_PROFILE": "profile-id", "GOTUN_LIVE_URL": "https://example.test/?token=fixture-secret"}, enabled: true},
		{name: "custom expected title", values: map[string]string{"GOTUN_LIVE_CONFIG": "config.json", "GOTUN_LIVE_PROFILE": "profile-id", "GOTUN_LIVE_URL": "https://example.test/", "GOTUN_LIVE_EXPECTED_TITLE": "Fixture"}, enabled: true, title: "Fixture"},
		{name: "plaintext rejected", values: map[string]string{"GOTUN_LIVE_CONFIG": "config.json", "GOTUN_LIVE_PROFILE": "profile-id", "GOTUN_LIVE_URL": "http://example.test/?token=fixture-secret"}, enabled: true, fail: true},
		{name: "credentials rejected", values: map[string]string{"GOTUN_LIVE_CONFIG": "config.json", "GOTUN_LIVE_PROFILE": "profile-id", "GOTUN_LIVE_URL": "https://fixture-user:fixture-secret@example.test/"}, enabled: true, fail: true},
		{name: "malformed URL rejected", values: map[string]string{"GOTUN_LIVE_CONFIG": "config.json", "GOTUN_LIVE_PROFILE": "profile-id", "GOTUN_LIVE_URL": "https://example.test/%fixture-secret"}, enabled: true, fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			options, enabled, err := liveOptions(func(key string) string { return tc.values[key] })
			if enabled != tc.enabled || (err != nil) != tc.fail {
				t.Fatalf("enabled=%v error=%v", enabled, err)
			}
			wantTitle := tc.title
			if wantTitle == "" {
				wantTitle = "IDE"
			}
			if enabled && err == nil && options.expectedTitle != wantTitle {
				t.Fatal("expected title override/default was not preserved")
			}
			if err != nil && strings.Contains(err.Error(), "fixture-secret") {
				t.Fatal("validation error disclosed a sensitive input")
			}
		})
	}
}

func TestLiveProfileReadsSelectedIDWithoutChangingConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	profile := service.SSHProfile{ID: "selected", Host: "fixture-host", User: "fixture-user", Password: "fixture-secret", JumpHostsList: []config.JumpHost{{Host: "jump", Port: "22", User: "jump-user", Password: "jump-secret"}}}
	data, err := json.Marshal(service.ConfigData{Profiles: []service.SSHProfile{{ID: "other"}, profile}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	selected, err := readLiveProfile(path, profile.ID)
	if err != nil || selected.Password != profile.Password || len(selected.JumpHostsList) != 1 || selected.JumpHostsList[0].Password != "jump-secret" {
		t.Fatal("selected profile did not preserve saved authentication and jump configuration")
	}
	if _, err := readLiveProfile(path, "missing"); err == nil {
		t.Fatal("missing live profile was accepted")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(data, after) {
		t.Fatal("reading live configuration changed its contents")
	}
	if _, err := readLiveProfile(filepath.Join(t.TempDir(), "fixture-secret"), "selected"); err == nil || strings.Contains(err.Error(), "fixture-secret") {
		t.Fatal("missing configuration was accepted or disclosed its path")
	}
	duplicates, err := json.Marshal(service.ConfigData{Profiles: []service.SSHProfile{profile, profile}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, duplicates, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readLiveProfile(path, profile.ID); err == nil {
		t.Fatal("ambiguous profile IDs were accepted")
	}
	if err := os.WriteFile(path, []byte("{fixture-secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readLiveProfile(path, "selected"); err == nil || strings.Contains(err.Error(), "fixture-secret") {
		t.Fatal("invalid configuration was accepted or disclosed its contents")
	}
}

func TestLiveHTTPSHarnessUsesSSHConnectAndVerifiesTLSAndHTML(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, tc := range []struct {
		name, contentType, body, title, jump string
		status                               int
		trusted, fail                        bool
	}{
		{name: "trusted TLS and expected HTML", contentType: "text/html; charset=utf-8", body: "<!doctype html><html><head><title> IDE </title></head><body>fixture</body></html>", title: "IDE", status: 200, trusted: true},
		{name: "untrusted TLS rejected", contentType: "text/html", body: "<title>IDE</title>", title: "IDE", status: 200, fail: true},
		{name: "bad status rejected", contentType: "text/html", body: "<title>IDE</title>", title: "IDE", status: 403, trusted: true, fail: true},
		{name: "JSON rejected", contentType: "application/json", body: `{"title":"IDE"}`, title: "IDE", status: 200, trusted: true, fail: true},
		{name: "wrong title rejected", contentType: "text/html", body: "<title>Login</title>", title: "IDE", status: 200, trusted: true, fail: true},
		{name: "custom escaped title accepted", contentType: "text/html", body: "<title>Fixture &amp; Tests</title>", title: "Fixture & Tests", status: 200, trusted: true},
		{name: "structured jump preserved", contentType: "text/html", body: "<title>IDE</title>", title: "IDE", status: 200, trusted: true, jump: "structured"},
		{name: "legacy jump preserved", contentType: "text/html", body: "<title>IDE</title>", title: "IDE", status: 200, trusted: true, jump: "legacy"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sshServer := testutil.NewSSHServer(t)
			testutil.TrustKnownHosts(t, home, sshServer)
			origin := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.TLS == nil || r.URL.Query().Get("token") != "fixture-secret" {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", tc.contentType)
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			}))
			origin.Config.ErrorLog = stdlog.New(io.Discard, "", 0)
			origin.StartTLS()
			defer origin.Close()
			var roots *x509.CertPool
			if tc.trusted {
				roots = x509.NewCertPool()
				roots.AddCert(origin.Certificate())
			}
			host, port, err := net.SplitHostPort(sshServer.Addr)
			if err != nil {
				t.Fatal(err)
			}
			profile := service.SSHProfile{ID: "fixture", Host: host, Port: port, User: "tester", Password: "test-password", KeyFile: sshServer.KeyFile, SystemProxy: true}
			var jump *testutil.SSHServer
			if tc.jump != "" {
				jump = testutil.NewSSHServer(t)
				testutil.TrustKnownHosts(t, home, jump)
				jumpHost, jumpPort, err := net.SplitHostPort(jump.Addr)
				if err != nil {
					t.Fatal(err)
				}
				if tc.jump == "structured" {
					profile.JumpHostsList = []config.JumpHost{{Host: jumpHost, Port: jumpPort, User: "tester", Password: "test-password"}}
				} else {
					profile.JumpHosts = []string{"tester@" + jump.Addr}
				}
			}
			result, err := checkProfileHTTPS(profile, origin.URL+"/?token=fixture-secret", tc.title, roots)
			if (err != nil) != tc.fail {
				t.Fatalf("HTTPS harness returned unexpected success/error: %v", err)
			}
			if err != nil && strings.Contains(err.Error(), "fixture-secret") {
				t.Fatal("request failure disclosed URL authorization data")
			}
			if !tc.fail && (result.status != http.StatusOK || result.host != "127.0.0.1") {
				t.Fatalf("unexpected destination/status: host=%q status=%d", result.host, result.status)
			}
			if sshServer.Forwarded() != 1 {
				t.Fatalf("TLS request did not use exactly one SSH CONNECT channel: %d", sshServer.Forwarded())
			}
			if jump != nil && jump.Forwarded() != 1 {
				t.Fatal("saved jump configuration was bypassed")
			}
		})
	}
}

func TestLiveHTTPSHarnessProtectsRedirectAuthorization(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, mode := range []string{"same origin", "different HTTPS origin", "HTTP downgrade"} {
		t.Run(mode, func(t *testing.T) {
			sshServer := testutil.NewSSHServer(t)
			testutil.TrustKnownHosts(t, home, sshServer)
			contacted := make(chan string, 1)
			destinationHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				contacted <- r.Referer()
				w.Header().Set("Content-Type", "text/html")
				io.WriteString(w, "<title>IDE</title>")
			})
			destination := httptest.NewTLSServer(destinationHandler)
			defer destination.Close()
			plaintext := httptest.NewServer(destinationHandler)
			defer plaintext.Close()
			var origin *httptest.Server
			origin = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/final" {
					destinationHandler.ServeHTTP(w, r)
					return
				}
				if r.URL.Query().Get("authorization") != "fixture-secret" {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				location := origin.URL + "/final"
				if mode == "different HTTPS origin" {
					location = destination.URL + "/final"
				} else if mode == "HTTP downgrade" {
					location = plaintext.URL + "/final"
				}
				http.Redirect(w, r, location, http.StatusFound)
			}))
			defer origin.Close()
			roots := x509.NewCertPool()
			roots.AddCert(origin.Certificate())
			roots.AddCert(destination.Certificate())
			host, port, err := net.SplitHostPort(sshServer.Addr)
			if err != nil {
				t.Fatal(err)
			}
			profile := service.SSHProfile{Host: host, Port: port, User: "tester", KeyFile: sshServer.KeyFile}
			_, err = checkProfileHTTPS(profile, origin.URL+"/?authorization=fixture-secret", "IDE", roots)
			if mode == "same origin" {
				if err != nil {
					t.Fatal(err)
				}
				if referer := <-contacted; referer != "" {
					t.Fatal("redirect disclosed the original authorization URL through Referer")
				}
			} else {
				if err == nil {
					t.Fatal("redirect beyond the initial HTTPS origin was accepted")
				}
				select {
				case <-contacted:
					t.Fatal("rejected redirect still contacted its destination")
				default:
				}
			}
		})
	}
}
