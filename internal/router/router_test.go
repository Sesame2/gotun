package router_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Sesame2/gotun/internal/router"
)

func ruleFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rules.yaml")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func newRouter(t *testing.T, content string) *router.Router {
	t.Helper()
	r, err := router.NewRouter(ruleFile(t, content))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestNewRouterErrors(t *testing.T) {
	tests := []struct{ name, path, content, want string }{
		{name: "empty path", want: "路径为空"},
		{name: "missing file", path: filepath.Join(t.TempDir(), "missing"), want: "读取规则文件失败"},
		{name: "invalid YAML", content: "rules: [", want: "解析YAML"},
		{name: "wrong rules type", content: "rules: {}", want: "解析YAML"},
		{name: "unknown rule", content: "rules:\n  - UNKNOWN,example.com,DIRECT\n", want: "未知的规则类型"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := tt.path
			if tt.content != "" {
				path = ruleFile(t, tt.content)
			}
			r, err := router.NewRouter(path)
			if err == nil || !strings.Contains(err.Error(), tt.want) || r != nil {
				t.Fatalf("NewRouter() = (%v,%v), want nil and error containing %q", r, err, tt.want)
			}
		})
	}
}

func TestMatchModes(t *testing.T) {
	tests := []struct {
		name, content string
		want          router.Action
	}{
		{"empty defaults to proxy", "", router.ActionProxy},
		{"rule default", "rules:\n - DOMAIN,example.com,DIRECT\n", router.ActionDirect},
		{"explicit rule", "mode: rule\nrules:\n - DOMAIN,example.com,REJECT\n", router.ActionReject},
		{"global overrides rules", "mode: global\nrules:\n - DOMAIN,example.com,REJECT\n", router.ActionProxy},
		{"direct overrides rules", "mode: direct\nrules:\n - DOMAIN,example.com,REJECT\n", router.ActionDirect},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRouter(t, tt.content)
			if got := r.Match("example.com:443"); got != tt.want {
				t.Fatalf("Match() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestMatchRuleOrderAndDefaults(t *testing.T) {
	r := newRouter(t, "rules:\n - DOMAIN,one.example,DIRECT\n - DOMAIN-SUFFIX,example,REJECT\n - DOMAIN,two.test\n - DOMAIN,three.test,unsupported\n")
	for host, want := range map[string]router.Action{
		"one.example":    router.ActionDirect,
		"sub.example":    router.ActionReject,
		"two.test":       router.ActionProxy,
		"three.test":     router.ActionProxy,
		"unmatched.test": router.ActionProxy,
	} {
		if got := r.Match(host); got != want {
			t.Errorf("Match(%q) = %s, want %s", host, got, want)
		}
	}
}

func TestMatchRuleBoundaries(t *testing.T) {
	tests := []struct {
		name, rule, host string
		want             router.Action
	}{
		{"exact domain", "DOMAIN,example.com,DIRECT", "example.com", router.ActionDirect},
		{"exact domain port", "DOMAIN,example.com,DIRECT", "example.com:443", router.ActionDirect},
		{"exact domain not subdomain", "DOMAIN,example.com,DIRECT", "www.example.com", router.ActionProxy},
		{"suffix root", "DOMAIN-SUFFIX,example.com,DIRECT", "example.com", router.ActionDirect},
		{"suffix subdomain", "DOMAIN-SUFFIX,example.com,DIRECT", "a.b.example.com:80", router.ActionDirect},
		{"suffix respects label boundary", "DOMAIN-SUFFIX,example.com,DIRECT", "badexample.com", router.ActionProxy},
		{"suffix does not match parent", "DOMAIN-SUFFIX,example.com,DIRECT", "com", router.ActionProxy},
		{"suffix does not match trailing domain", "DOMAIN-SUFFIX,example.com,DIRECT", "example.com.evil.test", router.ActionProxy},
		{"case insensitive exact", "DOMAIN,EXAMPLE.COM,DIRECT", "Example.Com", router.ActionDirect},
		{"case insensitive suffix", "DOMAIN-SUFFIX,EXAMPLE.COM,DIRECT", "WWW.Example.Com", router.ActionDirect},
		{"fully qualified domain", "DOMAIN,example.com,DIRECT", "example.com.", router.ActionDirect},
		{"keyword", "DOMAIN-KEYWORD,internal,REJECT", "my-internal.example", router.ActionReject},
		{"keyword no match", "DOMAIN-KEYWORD,internal,REJECT", "external.example", router.ActionProxy},
		{"keyword case insensitive", "DOMAIN-KEYWORD,INTERNAL,REJECT", "my-Internal.example", router.ActionReject},
		{"IPv4 network start", "IP-CIDR,192.0.2.0/24,DIRECT", "192.0.2.0", router.ActionDirect},
		{"IPv4 network end", "IP-CIDR,192.0.2.0/24,DIRECT", "192.0.2.255:443", router.ActionDirect},
		{"IPv4 below network", "IP-CIDR,192.0.2.0/24,DIRECT", "192.0.1.255", router.ActionProxy},
		{"IPv4 above network", "IP-CIDR,192.0.2.0/24,DIRECT", "192.0.3.0", router.ActionProxy},
		{"single IPv4", "IP-CIDR,192.0.2.7/32,REJECT", "192.0.2.7", router.ActionReject},
		{"all IPv4", "IP-CIDR,0.0.0.0/0,DIRECT", "203.0.113.3", router.ActionDirect},
		{"IP rule does not resolve DNS", "IP-CIDR,0.0.0.0/0,DIRECT", "example.com", router.ActionProxy},
		{"bad CIDR skipped", "IP-CIDR,invalid,DIRECT", "192.0.2.7", router.ActionProxy},
		{"IPv6 plain", "IP-CIDR6,2001:db8::/32,REJECT", "2001:db8::1", router.ActionReject},
		{"IPv6 with port", "IP-CIDR6,2001:db8::/32,REJECT", "[2001:db8::1]:443", router.ActionReject},
		{"IPv6 boundary", "IP-CIDR6,2001:db8::/32,REJECT", "2001:db9::", router.ActionProxy},
		{"case insensitive rule type", "domain,example.com,direct", "example.com", router.ActionDirect},
		{"rule whitespace", " DOMAIN , example.com , DIRECT ", "example.com", router.ActionDirect},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRouter(t, "rules:\n - '"+tt.rule+"'\n")
			if got := r.Match(tt.host); got != tt.want {
				t.Fatalf("Match(%q) = %s, want %s", tt.host, got, tt.want)
			}
		})
	}
}

func TestMatchFinalAction(t *testing.T) {
	for _, action := range []router.Action{router.ActionDirect, router.ActionReject, router.ActionProxy} {
		t.Run(string(action), func(t *testing.T) {
			r := newRouter(t, "rules:\n - DOMAIN,allowed.test,DIRECT\n - MATCH,"+string(action)+"\n - DOMAIN,late.test,DIRECT\n")
			if got := r.Match("unmatched.test"); got != action {
				t.Fatalf("MATCH,%s returned %s", action, got)
			}
			if got := r.Match("late.test"); got != action {
				t.Fatalf("MATCH did not stop evaluation: got %s, want %s", got, action)
			}
			if got := r.Match("allowed.test"); got != router.ActionDirect {
				t.Fatalf("earlier rule was overridden: %s", got)
			}
		})
	}
}

func FuzzDomainSuffixBoundary(f *testing.F) {
	for _, seed := range []string{"example.com", "www.example.com", "badexample.com", "EXAMPLE.COM:443", "example.com.", "example.com.evil.test", "", "[2001:db8::1]:443"} {
		f.Add(seed)
	}
	path := filepath.Join(f.TempDir(), "rules.yaml")
	if err := os.WriteFile(path, []byte("rules:\n - DOMAIN-SUFFIX,example.com,REJECT\n"), 0600); err != nil {
		f.Fatal(err)
	}
	r, err := router.NewRouter(path)
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, host string) {
		got := r.Match(host)
		if got != router.ActionProxy && got != router.ActionReject {
			t.Fatalf("unexpected action %q", got)
		}
		if strings.ContainsAny(host, ":\x00\r\n") || host == "" {
			return
		}
		// Prefixing a complete label keeps a domain inside the same suffix.
		if got == router.ActionReject && r.Match("sub."+host) != got {
			t.Fatalf("subdomain of %q escaped its suffix rule", host)
		}
	})
}
