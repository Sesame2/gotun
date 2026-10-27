package sysproxy

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/Sesame2/gotun/internal/logger"
)

func testProxyManager(t *testing.T, httpAddr, socksAddr string) *Manager {
	t.Helper()
	m := NewManager(logger.NewLogger(false), httpAddr, socksAddr)
	m.acquireLease = func() (func(), error) { return func() {}, nil }
	return m
}

type macProxyFixture struct {
	values map[string]string
	writes int
	fail   func([]string) bool
}

func newMacProxyFixture() *macProxyFixture {
	return &macProxyFixture{values: map[string]string{
		"webproxy":           "Enabled: Yes\nServer: legacy.proxy\nPort: 9000\nAuthenticated Proxy Enabled: 0\n",
		"securewebproxy":     "Enabled: No\nServer: secure.proxy\nPort: 9443\nAuthenticated Proxy Enabled: 0\n",
		"socksfirewallproxy": "Enabled: Yes\nServer: socks.proxy\nPort: 1081\nAuthenticated Proxy Enabled: 0\n",
		"proxybypassdomains": "*.corp.example\nlocalhost\n",
		"autoproxyurl":       "URL: https://corp.example/proxy.pac\nEnabled: Yes\n",
		"proxyautodiscovery": "Auto Proxy Discovery: On\n",
	}}
}

func (f *macProxyFixture) dictionary() map[string]any {
	dict := map[string]any{"ProxyAutoConfigURLString": macField(f.values["autoproxyurl"], "URL")}
	for kind, prefix := range map[string]string{"webproxy": "HTTP", "securewebproxy": "HTTPS", "socksfirewallproxy": "SOCKS"} {
		state := 0
		if macField(f.values[kind], "Enabled") == "Yes" {
			state = 1
		}
		var port int
		fmt.Sscan(macField(f.values[kind], "Port"), &port)
		dict[prefix+"Enable"], dict[prefix+"Proxy"], dict[prefix+"Port"], dict[prefix+"ProxyAuthenticated"] = state, macField(f.values[kind], "Server"), port, 0
	}
	dict["ExceptionsList"] = strings.Split(strings.TrimSpace(f.values["proxybypassdomains"]), "\n")
	dict["ProxyAutoConfigEnable"], dict["ProxyAutoDiscoveryEnable"] = 0, 0
	if macField(f.values["autoproxyurl"], "Enabled") == "Yes" {
		dict["ProxyAutoConfigEnable"] = 1
	}
	if macField(f.values["proxyautodiscovery"], "Auto Proxy Discovery") == "On" {
		dict["ProxyAutoDiscoveryEnable"] = 1
	}
	return dict
}

func (f *macProxyFixture) loadDictionary(dict map[string]any) {
	for kind, prefix := range map[string]string{"webproxy": "HTTP", "securewebproxy": "HTTPS", "socksfirewallproxy": "SOCKS"} {
		state := "No"
		if dict[prefix+"Enable"] == float64(1) {
			state = "Yes"
		}
		f.values[kind] = fmt.Sprintf("Enabled: %s\nServer: %s\nPort: %.0f\nAuthenticated Proxy Enabled: 0\n", state, dict[prefix+"Proxy"], dict[prefix+"Port"])
	}
	var bypass []string
	for _, value := range dict["ExceptionsList"].([]any) {
		bypass = append(bypass, value.(string))
	}
	f.values["proxybypassdomains"] = strings.Join(bypass, "\n") + "\n"
	state, discovery := "No", "Off"
	if dict["ProxyAutoConfigEnable"] == float64(1) {
		state = "Yes"
	}
	if dict["ProxyAutoDiscoveryEnable"] == float64(1) {
		discovery = "On"
	}
	f.values["autoproxyurl"] = "URL: " + dict["ProxyAutoConfigURLString"].(string) + "\nEnabled: " + state + "\n"
	f.values["proxyautodiscovery"] = "Auto Proxy Discovery: " + discovery + "\n"
}

func (f *macProxyFixture) run(name string, args ...string) ([]byte, error) {
	if name == "osascript" {
		script := args[len(args)-1]
		match := regexp.MustCompile(`var changes = (.*); ?
`).FindStringSubmatch(script)
		if len(match) == 0 {
			return json.Marshal(map[string]map[string]any{"service-wifi": f.dictionary()})
		}
		f.writes++
		if f.fail != nil && f.fail(args) {
			return nil, errors.New("fake permission denied")
		}
		var changes map[string]map[string]any
		if err := json.Unmarshal([]byte(match[1]), &changes); err != nil {
			return nil, err
		}
		f.loadDictionary(changes["service-wifi"])
		return nil, nil
	}
	return nil, fmt.Errorf("unexpected command %s", name)
}

func macField(output, name string) string {
	for _, line := range strings.Split(output, "\n") {
		if value, ok := strings.CutPrefix(line, name+": "); ok {
			return value
		}
	}
	return ""
}

func copyValues(values map[string]string) map[string]string {
	copy := make(map[string]string, len(values))
	for k, v := range values {
		copy[k] = v
	}
	return copy
}

func TestAuditOriginalMacProxyRestored(t *testing.T) {
	f := newMacProxyFixture()
	want := copyValues(f.values)
	m := testProxyManager(t, ":8080", ":1080")
	m.platform, m.run = "darwin", f.run
	if err := m.Enable(); err != nil {
		t.Fatal(err)
	}
	if err := m.Disable(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.values, want) {
		t.Fatalf("original proxy NOT restored:\ngot %#v\nwant %#v", f.values, want)
	}
}

func TestMacSnapshotFailureDoesNotWrite(t *testing.T) {
	f := newMacProxyFixture()
	m := testProxyManager(t, ":8080", ":1080")
	m.platform = "darwin"
	m.run = func(name string, args ...string) ([]byte, error) {
		if strings.Contains(args[len(args)-1], "JSON.stringify(readSnapshot())") {
			return nil, errors.New("snapshot denied")
		}
		return f.run(name, args...)
	}
	if m.Enable() == nil || f.writes != 0 || m.IsEnabled() {
		t.Fatalf("unsafe failed snapshot: writes=%d enabled=%v", f.writes, m.IsEnabled())
	}
}

func TestMacPartialEnableRestoresOriginalSnapshot(t *testing.T) {
	f := newMacProxyFixture()
	want := copyValues(f.values)
	m := testProxyManager(t, ":8080", ":1080")
	m.platform = "darwin"
	m.run = func(name string, args ...string) ([]byte, error) {
		out, err := f.run(name, args...)
		if err == nil && f.writes == 1 {
			return nil, errors.New("ApplyChanges failed after commit")
		}
		return out, err
	}
	if m.Enable() == nil {
		t.Fatal("partial enable reported success")
	}
	if m.IsEnabled() || !reflect.DeepEqual(f.values, want) {
		t.Fatalf("partial enable corrupted original settings: %#v", f.values)
	}
}

type windowsProxyFixture struct {
	Flags                      uint32
	Server, Bypass, AutoConfig string
}

// Replace only the PowerShell/WinINet host boundary.
func (f *windowsProxyFixture) run(name string, args ...string) ([]byte, error) {
	script := args[len(args)-1]
	match := regexp.MustCompile(`FromBase64String\('([^']+)'\)`).FindStringSubmatch(script)
	if len(match) != 0 {
		data, err := base64.StdEncoding.DecodeString(match[1])
		if err != nil {
			return nil, err
		}
		return nil, json.Unmarshal(data, f)
	}
	if strings.Contains(script, "::Query()") {
		return json.Marshal(f)
	}
	return nil, nil
}

func TestWindowsOriginalProxyAndPACRestored(t *testing.T) {
	f := &windowsProxyFixture{Flags: 15, Server: "http=old.proxy:8000;https=secure.proxy:9443;socks=socks.proxy:1081", Bypass: "<local>;*.corp.example", AutoConfig: "https://corp.example/proxy.pac"}
	want := *f
	m := testProxyManager(t, ":8080", ":1080")
	m.platform, m.run = "windows", f.run
	if err := m.Enable(); err != nil {
		t.Fatal(err)
	}
	if err := m.Disable(); err != nil {
		t.Fatal(err)
	}
	if *f != want {
		t.Fatalf("Windows proxy/PAC not restored: got %+v want %+v", *f, want)
	}
}

func TestAuditLinuxCommandFailure(t *testing.T) {
	m := testProxyManager(t, ":8080", "")
	m.platform = "linux"
	m.run = func(string, ...string) ([]byte, error) { return nil, errors.New("fake permission denied") }
	if err := m.Enable(); err == nil {
		t.Fatalf("Enable returned success despite every gsettings command failing; enabled=%v", m.IsEnabled())
	}
}

func TestMacAbsentFieldsAndAuthenticationRoundTrip(t *testing.T) {
	want := macSettings{"blank": nil, "empty": {}, "mixed": {
		"HTTPEnable": float64(0), "HTTPProxy": "old.proxy", "HTTPPort": float64(9000), "HTTPProxyAuthenticated": float64(1),
		"HTTPSEnable": float64(1), "HTTPSProxy": "secure.proxy", "HTTPSPort": float64(443),
		"SOCKSEnable": float64(0), "SOCKSProxy": "socks.proxy", "SOCKSPort": float64(1080),
		"ExceptionsList": []any{"*.corp.example", "127.0.0.1"}, "ProxyAutoConfigURLString": "https://corp.example/自动.pac", "ProxyAutoConfigEnable": float64(1),
		"ProxyAutoDiscoveryEnable": float64(1), "ExcludeSimpleHostnames": float64(1),
	}}
	data, _ := json.Marshal(want)
	var actual macSettings
	json.Unmarshal(data, &actual)
	m := testProxyManager(t, ":8080", "")
	m.platform = "darwin"
	m.run = func(name string, args ...string) ([]byte, error) {
		script := args[len(args)-1]
		match := regexp.MustCompile(`var changes = (.*); ?\n`).FindStringSubmatch(script)
		if len(match) == 0 {
			return json.Marshal(actual)
		}
		return nil, json.Unmarshal([]byte(match[1]), &actual)
	}
	if err := m.Enable(); err != nil {
		t.Fatal(err)
	}
	if err := m.Disable(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, want) {
		t.Fatalf("lost absent fields/authentication/PAC: %#v", actual)
	}
}

func TestWindowsMixedProxyModesRoundTrip(t *testing.T) {
	for _, flags := range []uint32{1, 3, 5, 9, 13, 15} {
		t.Run(fmt.Sprint(flags), func(t *testing.T) {
			f := &windowsProxyFixture{Flags: flags, Server: "http=legacy.proxy:8000;https=secure.proxy:443;socks=socks.proxy:1080", Bypass: "<local>;*.corp.example", AutoConfig: "https://corp.example/自动.pac"}
			want := *f
			m := testProxyManager(t, ":8080", "")
			m.platform, m.run = "windows", f.run
			if err := m.Enable(); err != nil {
				t.Fatal(err)
			}
			if f.Flags != 3 || f.AutoConfig != "" || f.Bypass != "" {
				t.Fatalf("PAC/WPAD or bypass still active: %+v", f)
			}
			if err := m.Disable(); err != nil {
				t.Fatal(err)
			}
			if *f != want {
				t.Fatalf("not restored: %+v", f)
			}
		})
	}
}

func TestWindowsPartialEnableAndFailedRestoreAreRetryable(t *testing.T) {
	f := &windowsProxyFixture{Flags: 15, Server: "legacy.proxy:9000", Bypass: "*.corp.example", AutoConfig: "https://corp.example/proxy.pac"}
	want := *f
	m := testProxyManager(t, ":8080", ":1080")
	m.platform = "windows"
	writes := 0
	denyRestore := true
	m.run = func(name string, args ...string) ([]byte, error) {
		if strings.Contains(args[len(args)-1], "FromBase64String") {
			writes++
			if writes == 1 {
				_, err := f.run(name, args...)
				if err != nil {
					return nil, err
				}
				return nil, errors.New("notification failed after write")
			}
			if denyRestore {
				return nil, errors.New("restore denied")
			}
		}
		return f.run(name, args...)
	}
	if m.Enable() == nil || m.IsEnabled() || !m.NeedsCleanup() {
		t.Fatal("failed enable lost pending rollback or falsely reported enabled")
	}
	if m.Enable() == nil {
		t.Fatal("retry replaced the pending original snapshot")
	}
	denyRestore = false
	if err := m.Disable(); err != nil {
		t.Fatal(err)
	}
	if *f != want || m.NeedsCleanup() {
		t.Fatalf("retry did not restore original snapshot: %+v", f)
	}
}

func TestMacRestoreFailureRetainsSnapshot(t *testing.T) {
	f := newMacProxyFixture()
	want := copyValues(f.values)
	m := testProxyManager(t, ":8080", ":1080")
	m.platform, m.run = "darwin", f.run
	if err := m.Enable(); err != nil {
		t.Fatal(err)
	}
	f.fail = func([]string) bool { return true }
	if m.Disable() == nil || !m.NeedsCleanup() {
		t.Fatal("failed restore lost snapshot")
	}
	f.fail = nil
	if err := m.Disable(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.values, want) {
		t.Fatalf("retry lost original settings: %#v", f.values)
	}
}

func TestExplicitClearDoesNotRestoreAnEarlierProxy(t *testing.T) {
	for _, platform := range []string{"darwin", "windows"} {
		t.Run(platform, func(t *testing.T) {
			m := testProxyManager(t, ":8080", ":1080")
			m.platform = platform
			mac := newMacProxyFixture()
			win := &windowsProxyFixture{Flags: 15, Server: "legacy.proxy:9000", AutoConfig: "https://corp.example/proxy.pac"}
			if platform == "darwin" {
				m.run = mac.run
			} else {
				m.run = win.run
			}
			// Clear works even when this manager never enabled the existing OS proxy.
			if err := m.Clear(); err != nil {
				t.Fatal(err)
			}
			if platform == "darwin" {
				if macField(mac.values["webproxy"], "Enabled") != "No" || macField(mac.values["socksfirewallproxy"], "Enabled") != "No" || macField(mac.values["autoproxyurl"], "Enabled") != "No" || macField(mac.values["proxyautodiscovery"], "Auto Proxy Discovery") != "Off" {
					t.Fatal(mac.values)
				}
			} else if win.Flags != 1 || win.Server != "" || win.AutoConfig != "" {
				t.Fatal(win)
			}
			if err := m.Enable(); err != nil {
				t.Fatal(err)
			}
			if err := m.Clear(); err != nil {
				t.Fatal(err)
			}
			if err := m.Disable(); err != nil {
				t.Fatal(err)
			}
			if m.IsEnabled() || m.NeedsCleanup() {
				t.Fatal("clear retained an earlier restoration intent")
			}
			if platform == "windows" && win.Flags != 1 {
				t.Fatal("Stop resurrected original proxy")
			}
		})
	}
}

func TestClearFailureRetriesClearInsteadOfRestore(t *testing.T) {
	f := &windowsProxyFixture{Flags: 15, Server: "old.proxy", AutoConfig: "https://corp.example/a.pac"}
	m := testProxyManager(t, ":8080", "")
	m.platform, m.run = "windows", f.run
	if err := m.Enable(); err != nil {
		t.Fatal(err)
	}
	m.run = func(string, ...string) ([]byte, error) { return nil, errors.New("clear denied") }
	if m.Clear() == nil || !m.NeedsCleanup() {
		t.Fatal("Clear failure lost cleanup intent")
	}
	m.run = f.run
	if err := m.Disable(); err != nil {
		t.Fatal(err)
	}
	if f.Flags != 1 || f.Server != "" || f.AutoConfig != "" {
		t.Fatal("Clear retry restored an unwanted proxy", f)
	}
}

func TestManagerEnableDisableAreIdempotent(t *testing.T) {
	for _, platform := range []string{"darwin", "windows"} {
		t.Run(platform, func(t *testing.T) {
			m := testProxyManager(t, ":8080", ":1080")
			m.platform = platform
			var run func(string, ...string) ([]byte, error)
			if platform == "darwin" {
				run = newMacProxyFixture().run
			} else {
				run = (&windowsProxyFixture{Flags: 1}).run
			}
			calls := 0
			m.run = func(name string, args ...string) ([]byte, error) { calls++; return run(name, args...) }
			if err := m.Enable(); err != nil {
				t.Fatal(err)
			}
			before := calls
			if err := m.Enable(); err != nil || calls != before {
				t.Fatal("Enable not idempotent", err)
			}
			if err := m.Disable(); err != nil {
				t.Fatal(err)
			}
			before = calls
			if err := m.Disable(); err != nil || calls != before {
				t.Fatal("Disable not idempotent", err)
			}
		})
	}
}

func TestSnapshotFailureAcrossPlatformsNeverWrites(t *testing.T) {
	for _, platform := range []string{"darwin", "windows", "linux"} {
		t.Run(platform, func(t *testing.T) {
			m := testProxyManager(t, ":8080", "")
			m.platform = platform
			calls := 0
			m.run = func(string, ...string) ([]byte, error) { calls++; return nil, errors.New("snapshot denied") }
			if m.Enable() == nil || calls != 1 || m.IsEnabled() || m.NeedsCleanup() {
				t.Fatal("snapshot failure wrote or retained invalid cleanup", calls)
			}
		})
	}
}

func TestLinuxProxyAndEnvironmentRoundTripAndErrors(t *testing.T) {
	for _, failAt := range []int{0, 1, 2, 3, 4, 5, 6, 7, 8} {
		t.Run(fmt.Sprint(failAt), func(t *testing.T) {
			t.Setenv("http_proxy", "http://legacy.proxy:9000")
			t.Setenv("https_proxy", "")
			os.Unsetenv("https_proxy")
			t.Setenv("no_proxy", "localhost,*.corp.example")
			original := []string{"'auto'", "['localhost', '*.corp.example']", "'old.proxy'", "9000", "'secure.proxy'", "9443", "'socks.proxy'", "1081"}
			values := append([]string(nil), original...)
			writes := 0
			m := testProxyManager(t, ":8080", ":1080")
			m.platform = "linux"
			m.run = func(name string, args ...string) ([]byte, error) {
				for i, key := range gnomeProxyKeys {
					if args[1] != key[0] || args[2] != key[1] {
						continue
					}
					if args[0] == "get" {
						return []byte(values[i]), nil
					}
					writes++
					if writes == failAt {
						return nil, errors.New("gsettings denied")
					}
					values[i] = args[3]
					return nil, nil
				}
				return nil, fmt.Errorf("unexpected command %s %v", name, args)
			}
			err := m.Enable()
			if failAt > 0 && err == nil {
				t.Fatal("write error suppressed")
			}
			if failAt == 0 && err != nil {
				t.Fatal(err)
			}
			if failAt > 0 && m.IsEnabled() {
				t.Fatal("failed enable reports enabled")
			}
			if err := m.Disable(); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(values, original) || os.Getenv("http_proxy") != "http://legacy.proxy:9000" || os.Getenv("no_proxy") != "localhost,*.corp.example" {
				t.Fatal("Linux snapshot lost", values)
			}
			if _, ok := os.LookupEnv("https_proxy"); ok {
				t.Fatal("unset environment variable restored as set")
			}
		})
	}
}

func TestLinuxEverySnapshotReadMustSucceed(t *testing.T) {
	for failAt := 1; failAt <= len(gnomeProxyKeys); failAt++ {
		t.Run(fmt.Sprint(failAt), func(t *testing.T) {
			m := testProxyManager(t, ":8080", "")
			m.platform = "linux"
			reads, writes := 0, 0
			m.run = func(_ string, args ...string) ([]byte, error) {
				if args[0] == "get" {
					reads++
					if reads == failAt {
						return nil, errors.New("denied")
					}
					return []byte("'snapshot'"), nil
				}
				writes++
				return nil, nil
			}
			if m.Enable() == nil || writes != 0 || m.NeedsCleanup() {
				t.Fatal("incomplete snapshot wrote settings", writes)
			}
		})
	}
}

func TestLinuxRestoreAndClearFailuresRemainRetryable(t *testing.T) {
	t.Setenv("http_proxy", "http://old.proxy:9000")
	t.Setenv("https_proxy", "http://secure.proxy:9443")
	t.Setenv("no_proxy", "localhost")
	original := []string{"'auto'", "['localhost']", "'old.proxy'", "9000", "'secure.proxy'", "9443", "'socks.proxy'", "1081"}
	values := append([]string(nil), original...)
	m := testProxyManager(t, ":8080", ":1080")
	m.platform = "linux"
	deny := false
	m.run = func(_ string, args ...string) ([]byte, error) {
		for i, key := range gnomeProxyKeys {
			if args[1] != key[0] || args[2] != key[1] {
				continue
			}
			if args[0] == "get" {
				return []byte(values[i]), nil
			}
			if deny {
				return nil, errors.New("denied")
			}
			values[i] = args[3]
			return nil, nil
		}
		return nil, fmt.Errorf("unexpected gsettings command %v", args)
	}
	if err := m.Enable(); err != nil {
		t.Fatal(err)
	}
	deny = true
	if m.Disable() == nil || !m.NeedsCleanup() {
		t.Fatal("restore failure lost snapshot")
	}
	deny = false
	if err := m.Disable(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(values, original) {
		t.Fatal(values)
	}
	deny = true
	if m.Clear() == nil || !m.NeedsCleanup() {
		t.Fatal("clear failure suppressed")
	}
	deny = false
	if err := m.Disable(); err != nil {
		t.Fatal(err)
	}
	if values[0] != "'none'" || m.NeedsCleanup() {
		t.Fatal("failed clear did not retry clear")
	}
}
