package tun

import (
	"net"
	"testing"

	"github.com/Sesame2/gotun/internal/config"
)

func TestNewTunServiceAddressCalculation(t *testing.T) {
	tests := []struct{ cidr, ip, mask, peer string }{
		{"10.0.0.1/24", "10.0.0.1", "255.255.255.0", "10.0.0.2"},
		{"172.16.4.8/16", "172.16.4.8", "255.255.0.0", "172.16.4.9"},
		{"192.0.2.10/32", "192.0.2.10", "255.255.255.255", "192.0.2.11"},
		{"10.0.0.255/8", "10.0.0.255", "255.0.0.0", "10.0.1.0"},
		{"10.0.255.255/8", "10.0.255.255", "255.0.0.0", "10.1.0.0"},
	}
	for _, tt := range tests {
		t.Run(tt.cidr, func(t *testing.T) {
			cfg := config.NewConfig()
			cfg.TunCIDR = tt.cidr
			service, err := NewTunService(cfg, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			if service.tunIP != tt.ip || service.tunMask != tt.mask || service.peerIP != tt.peer {
				t.Fatalf("address = (%s,%s,%s), want (%s,%s,%s)", service.tunIP, service.tunMask, service.peerIP, tt.ip, tt.mask, tt.peer)
			}
			if service.dev != nil || service.stack != nil || service.endpoint != nil {
				t.Fatal("constructor started a device or network stack")
			}
		})
	}
}

func TestNewTunServiceRejectsInvalidAddress(t *testing.T) {
	for _, cidr := range []string{"", "not-cidr", "10.0.0.1", "10.0.0.1/33", "2001:db8::1/64"} {
		t.Run(cidr, func(t *testing.T) {
			cfg := config.NewConfig()
			cfg.TunCIDR = cidr
			service, err := NewTunService(cfg, nil, nil)
			if err == nil || service != nil {
				t.Fatalf("NewTunService(%q) = (%v,%v), want nil and error", cidr, service, err)
			}
		})
	}
}

func TestTunServiceCloseBeforeStart(t *testing.T) {
	service, err := NewTunService(config.NewConfig(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := service.Close(); err != nil {
			t.Fatalf("Close() = %v", err)
		}
	}
}

func TestIPv4Arithmetic(t *testing.T) {
	tests := []struct {
		base   string
		offset uint32
		want   string
	}{
		{"10.0.0.0", 5, "10.0.0.5"},
		{"192.0.2.255", 1, "192.0.3.0"},
		{"10.0.255.255", 1, "10.1.0.0"},
		{"0.0.0.0", 0xffffffff, "255.255.255.255"},
		{"255.255.255.255", 1, "0.0.0.0"},
	}
	for _, tt := range tests {
		t.Run(tt.base+"/"+tt.want, func(t *testing.T) {
			for _, base := range []net.IP{net.ParseIP(tt.base), net.ParseIP(tt.base).To4()} {
				before := append(net.IP(nil), base...)
				got := ipAdd(base, tt.offset)
				if got.String() != tt.want {
					t.Fatalf("%s + %d = %s, want %s", base, tt.offset, got, tt.want)
				}
				if offset := ipSub(got, base); offset != tt.offset {
					t.Fatalf("inverse offset = %d, want %d", offset, tt.offset)
				}
				if !base.Equal(before) {
					t.Fatal("arithmetic mutated its input")
				}
			}
		})
	}
}

func TestSubnetAliasPreservesHostOffset(t *testing.T) {
	alias, err := config.ParseSubnetAlias("10.0.0.0/16:172.16.0.0/16")
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ source, target string }{{"10.0.0.0", "172.16.0.0"}, {"10.0.1.42", "172.16.1.42"}, {"10.0.255.255", "172.16.255.255"}} {
		offset := ipSub(net.ParseIP(tt.source), alias.Src.IP)
		got := ipAdd(alias.Dst.IP, offset)
		if got.String() != tt.target || !alias.Dst.Contains(got) {
			t.Errorf("alias %s = %s, want %s", tt.source, got, tt.target)
		}
	}
}

func FuzzIPv4Arithmetic(f *testing.F) {
	for _, seed := range [][2]uint32{{0, 0}, {0xffffffff, 1}, {0x0a00ffff, 1}, {0xc0000200, 255}, {1, 0xffffffff}} {
		f.Add(seed[0], seed[1])
	}
	f.Fuzz(func(t *testing.T, base, offset uint32) {
		ip := uint32ToIP(base)
		if got := ipToUint32(ip); got != base {
			t.Fatalf("IPv4 round trip = %d, want %d", got, base)
		}
		if got := ipToUint32(net.ParseIP(ip.String())); got != base {
			t.Fatalf("text IPv4 round trip = %d, want %d", got, base)
		}
		if got := ipSub(ipAdd(ip, offset), ip); got != offset {
			t.Fatalf("add/sub inverse = %d, want %d", got, offset)
		}
	})
}
