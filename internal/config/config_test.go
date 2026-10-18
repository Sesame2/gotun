package config_test

import (
	"net"
	"strings"
	"testing"
	"time"

	"github.com/Sesame2/gotun/internal/config"
)

func validConfig() *config.Config {
	cfg := config.NewConfig()
	cfg.SSHServer = "ssh.example.test:22"
	cfg.SSHUser = "alice"
	return cfg
}

func TestNewConfigDefaults(t *testing.T) {
	cfg := config.NewConfig()
	if cfg.ListenAddr != ":8080" || cfg.SSHPort != "22" || cfg.Timeout != 10*time.Second {
		t.Fatalf("unexpected connection defaults: %+v", cfg)
	}
	if !cfg.InteractiveAuth || !cfg.SystemProxy || cfg.TunMode || cfg.TunGlobal || cfg.Verbose {
		t.Fatalf("unexpected mode defaults: %+v", cfg)
	}
	if cfg.TunCIDR != "10.0.0.1/24" || cfg.SocksAddr != "" || cfg.RuleFile != "" {
		t.Fatalf("unexpected optional defaults: %+v", cfg)
	}
	cfg.JumpHostsList = append(cfg.JumpHostsList, config.JumpHost{Host: "one"})
	cfg.TunRoute = append(cfg.TunRoute, "10.0.0.0/8")
	other := config.NewConfig()
	if len(other.JumpHostsList) != 0 || len(other.TunRoute) != 0 {
		t.Fatal("default configurations share mutable state")
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name      string
		edit      func(*config.Config)
		wantError string
	}{
		{"interactive auth", func(*config.Config) {}, ""},
		{"missing server", func(c *config.Config) { c.SSHServer = "" }, "SSH服务器"},
		{"missing user", func(c *config.Config) { c.SSHUser = "" }, "SSH用户名"},
		{"missing credentials", func(c *config.Config) { c.InteractiveAuth = false }, "SSH密码"},
		{"password auth", func(c *config.Config) { c.InteractiveAuth = false; c.SSHPassword = "secret" }, ""},
		{"private key auth", func(c *config.Config) { c.InteractiveAuth = false; c.SSHKeyFile = "/key" }, ""},
		{"legacy jump", func(c *config.Config) { c.JumpHosts = []string{"alice@bastion:2222", "other"} }, ""},
		{"malformed legacy jump", func(c *config.Config) { c.JumpHosts = []string{"a@b@c"} }, "跳板机"},
		{"legacy missing host", func(c *config.Config) { c.JumpHosts = []string{"alice@:22"} }, "跳板机"},
		{"structured jump", func(c *config.Config) {
			c.JumpHostsList = []config.JumpHost{{Host: "bastion", User: "bob", Port: "2222"}}
		}, ""},
		{"structured missing host", func(c *config.Config) { c.JumpHostsList = []config.JumpHost{{User: "bob"}} }, "第 1 个跳板机"},
		{"structured missing user", func(c *config.Config) { c.JumpHostsList = []config.JumpHost{{Host: "bastion"}} }, "用户名"},
		{"second structured invalid", func(c *config.Config) { c.JumpHostsList = []config.JumpHost{{Host: "one", User: "bob"}, {Host: "two"}} }, "第 2 个跳板机"},
		{"structured invalid port", func(c *config.Config) {
			c.JumpHostsList = []config.JumpHost{{Host: "bastion", User: "bob", Port: "65536"}}
		}, "端口"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.edit(cfg)
			err := cfg.Validate()
			if tt.wantError == "" {
				if err != nil {
					t.Fatalf("Validate() = %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("Validate() = %v, want error containing %q", err, tt.wantError)
			}
		})
	}
}

func TestValidateDefaultsStructuredJumpPort(t *testing.T) {
	cfg := validConfig()
	cfg.JumpHostsList = []config.JumpHost{{Host: "one", User: "alice"}, {Host: "two", User: "bob", Port: "2222"}}
	for range 2 {
		if err := cfg.Validate(); err != nil {
			t.Fatal(err)
		}
		if cfg.JumpHostsList[0].Port != "22" || cfg.JumpHostsList[1].Port != "2222" {
			t.Fatalf("Validate() changed explicit port or omitted default: %+v", cfg.JumpHostsList)
		}
	}
}

func TestGetJumpHostInfo(t *testing.T) {
	tests := []struct {
		input, user, host, port string
		invalid                 bool
	}{
		{input: "bastion", host: "bastion", port: "22"},
		{input: "alice@bastion", user: "alice", host: "bastion", port: "22"},
		{input: "bastion:2222", host: "bastion", port: "2222"},
		{input: "alice@192.0.2.1:1", user: "alice", host: "192.0.2.1", port: "1"},
		{input: "alice@bastion:65535", user: "alice", host: "bastion", port: "65535"},
		{input: "", invalid: true}, {input: "alice@", invalid: true},
		{input: "alice@:22", invalid: true}, {input: "a@b@c", invalid: true},
		{input: "host:22:33", invalid: true},
		{input: "host:", invalid: true}, {input: "host:0", invalid: true},
		{input: "host:-1", invalid: true}, {input: "host:65536", invalid: true},
		{input: "host:abc", invalid: true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			user, host, port, err := config.NewConfig().GetJumpHostInfo(tt.input)
			if tt.invalid {
				if err == nil {
					t.Fatalf("GetJumpHostInfo(%q) accepted malformed endpoint", tt.input)
				}
				return
			}
			if err != nil || user != tt.user || host != tt.host || port != tt.port {
				t.Fatalf("GetJumpHostInfo(%q) = (%q,%q,%q,%v), want (%q,%q,%q,nil)", tt.input, user, host, port, err, tt.user, tt.host, tt.port)
			}
		})
	}
}

func TestParseSubnetAlias(t *testing.T) {
	tests := []struct {
		input, src, dst string
		invalid         bool
	}{
		{input: "10.0.0.0/24:192.168.1.0/24", src: "10.0.0.0/24", dst: "192.168.1.0/24"},
		{input: "10.0.0.27/24:192.168.1.99/24", src: "10.0.0.0/24", dst: "192.168.1.0/24"},
		{input: "10.0.0.5/32:192.0.2.1/32", src: "10.0.0.5/32", dst: "192.0.2.1/32"},
		{input: "10.0.0.0/24:172.16.0.0/16", src: "10.0.0.0/24", dst: "172.16.0.0/16"},
		{input: "0.0.0.0/0:0.0.0.0/0", src: "0.0.0.0/0", dst: "0.0.0.0/0"},
		{input: "", invalid: true}, {input: "10.0.0.0/24", invalid: true},
		{input: "10.0.0.0/24:192.0.2.0/24:extra", invalid: true},
		{input: "bad:192.0.2.0/24", invalid: true}, {input: "10.0.0.0/24:bad", invalid: true},
		{input: "10.0.0.0/33:192.0.2.0/24", invalid: true},
		{input: "10.0.0.0/24:192.0.2.0/25", invalid: true},
		{input: "2001:db8::/64:2001:db8:1::/64", invalid: true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			alias, err := config.ParseSubnetAlias(tt.input)
			if tt.invalid {
				if err == nil {
					t.Fatalf("ParseSubnetAlias(%q) accepted invalid mapping", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if alias.Src.String() != tt.src || alias.Dst.String() != tt.dst {
				t.Fatalf("ParseSubnetAlias(%q) = %v, want %s -> %s", tt.input, alias, tt.src, tt.dst)
			}
		})
	}
}

func FuzzGetJumpHostInfo(f *testing.F) {
	for _, seed := range []string{"alice@host:22", "host", "", "a@b@c", "host:65536", "host:"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		cfg := config.NewConfig()
		user, host, port, err := cfg.GetJumpHostInfo(input)
		if err != nil {
			return
		}
		if host == "" || port == "" {
			t.Fatalf("successful parse returned empty endpoint: %q", input)
		}
		canonical := host + ":" + port
		if user != "" {
			canonical = user + "@" + canonical
		}
		u2, h2, p2, err := cfg.GetJumpHostInfo(canonical)
		if err != nil || user != u2 || host != h2 || port != p2 {
			t.Fatalf("round trip failed for %q", input)
		}
	})
}

func FuzzParseSubnetAlias(f *testing.F) {
	for _, seed := range []string{"10.0.0.0/24:192.168.1.0/24", "0.0.0.0/0:0.0.0.0/0", "10.0.0.0/24:192.0.2.0/25", "bad", "::"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		alias, err := config.ParseSubnetAlias(input)
		if err != nil {
			return
		}
		if alias.Src == nil || alias.Dst == nil || alias.Src.IP.To4() == nil || alias.Dst.IP.To4() == nil {
			t.Fatalf("successful alias is not IPv4: %q", input)
		}
		for _, network := range []*net.IPNet{alias.Src, alias.Dst} {
			if !network.Contains(network.IP) {
				t.Fatalf("network excludes its base: %q", input)
			}
		}
		canonical := alias.Src.String() + ":" + alias.Dst.String()
		again, err := config.ParseSubnetAlias(canonical)
		if err != nil || again.Src.String() != alias.Src.String() || again.Dst.String() != alias.Dst.String() {
			t.Fatalf("canonical alias failed round trip: %q", input)
		}
	})
}
