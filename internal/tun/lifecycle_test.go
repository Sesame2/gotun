package tun

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/Sesame2/gotun/internal/config"
	"github.com/Sesame2/gotun/internal/logger"
	wgtun "golang.zx2c4.com/wireguard/tun"
)

func TestNativeCommandCleanupSurvivesCancelledContext(t *testing.T) {
	if os.Getenv("GOTUN_TEST_CLEANUP_COMMAND") == "1" {
		fmt.Print("cleanup executed")
		return
	}
	t.Setenv("GOTUN_TEST_CLEANUP_COMMAND", "1")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	service := &TunService{cfg: &config.Config{Context: ctx}}
	args := []string{"-test.run=^TestNativeCommandCleanupSurvivesCancelledContext$"}
	if _, err := service.runCommand(executable, args...); !errors.Is(err, context.Canceled) {
		t.Fatalf("startup command ignored cancellation: %v", err)
	}
	service.cleaning = true
	if output, err := service.runCommand(executable, args...); err != nil || !strings.Contains(string(output), "cleanup executed") {
		t.Fatalf("cancelled startup prevented cleanup command: %v, %s", err, output)
	}
}

func TestCancelledNativeStartupRollsBackRoutes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg := config.NewConfig()
	cfg.Context, cfg.TunRoute = ctx, []string{"203.0.113.0/24", "198.51.100.0/24"}
	var calls []string
	service, device := nativeWithFakeNetwork(t, "darwin", cfg, func(name string, args ...string) ([]byte, error) {
		call := name + " " + strings.Join(args, " ")
		calls = append(calls, call)
		if call == "route add 203.0.113.0/24 -interface memory-tun" {
			cancel()
		}
		return nil, nil
	})
	if err := service.Start(); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Start = %v", err)
	}
	joined := strings.Join(calls, "\n")
	if !strings.Contains(joined, "route delete 203.0.113.0/24 -interface memory-tun") || strings.Contains(joined, "add 198.51.100.0") {
		t.Fatalf("cancelled startup failed to restore routes: %s", joined)
	}
	select {
	case <-device.closed:
	default:
		t.Fatal("cancelled startup left device open")
	}
}

type retryCloseDevice struct {
	*memoryDevice
	failure error
	closes  int
}

type physicalSSH struct{ remote net.Addr }

func (s physicalSSH) RemoteAddr() net.Addr { return s.remote }
func (s physicalSSH) Dial(string, string) (net.Conn, error) {
	return nil, errors.New("no forwarded traffic in lifecycle tests")
}

func TestGlobalRoutesUseActualSSHPeerAndRemoveBypassOnClose(t *testing.T) {
	for _, platform := range []string{"darwin", "linux", "windows"} {
		t.Run(platform, func(t *testing.T) {
			cfg := config.NewConfig()
			cfg.SSHServer, cfg.TunGlobal = "198.51.100.9:22", true
			var calls []string
			service, _ := nativeWithFakeNetwork(t, platform, cfg, func(name string, args ...string) ([]byte, error) {
				call := name + " " + strings.Join(args, " ")
				calls = append(calls, call)
				switch {
				case call == "route -n get 203.0.113.9":
					return []byte("gateway: 192.0.2.1\ninterface: en0\n"), nil
				case call == "ip -4 route get 203.0.113.9":
					return []byte("203.0.113.9 via 192.0.2.1 dev en0 src 192.0.2.3\n"), nil
				case name == "powershell" && strings.Contains(call, "Find-NetRoute -RemoteIPAddress '203.0.113.9'"):
					return []byte(`{"NextHop":"192.0.2.1","InterfaceIndex":12}`), nil
				}
				return nil, nil
			})
			service.ssh = physicalSSH{remote: &net.TCPAddr{IP: net.ParseIP("203.0.113.9"), Port: 2200}}
			if err := service.Start(); err != nil {
				t.Fatal(err)
			}
			if err := service.Close(); err != nil {
				t.Fatal(err)
			}
			joined := strings.Join(calls, "\n")
			wantBypass := map[string]string{
				"darwin":  "route add 203.0.113.9/32 192.0.2.1 -ifp en0",
				"linux":   "ip route add 203.0.113.9 via 192.0.2.1 dev en0",
				"windows": "route add 203.0.113.9 mask 255.255.255.255 192.0.2.1 METRIC 1 IF 12",
			}[platform]
			wantDelete := map[string]string{
				"darwin":  "route delete 203.0.113.9/32 192.0.2.1 -ifp en0",
				"linux":   "ip route del 203.0.113.9 via 192.0.2.1 dev en0",
				"windows": "route delete 203.0.113.9 mask 255.255.255.255 192.0.2.1 IF 12",
			}[platform]
			if !strings.Contains(joined, wantBypass) || !strings.Contains(joined, wantDelete) || strings.Contains(joined, "198.51.100.9") {
				t.Fatalf("wrong bypass or missing cleanup: %s", joined)
			}
		})
	}
}

func TestRouteConflictReturnsStartupErrorWithoutExiting(t *testing.T) {
	if os.Getenv("GOTUN_TEST_ROUTE_CONFLICT") == "1" {
		cfg := config.NewConfig()
		cfg.SSHServer, cfg.TunRoute = "127.0.0.1:22", []string{"127.0.0.0/8"}
		service, device := nativeWithFakeNetwork(t, "darwin", cfg, func(string, ...string) ([]byte, error) { return nil, nil })
		service.ssh = physicalSSH{remote: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 22}}
		if err := service.Start(); err == nil || !strings.Contains(err.Error(), "127.0.0.1") {
			t.Fatalf("conflicting startup error = %v", err)
		}
		select {
		case <-device.closed:
		default:
			t.Fatal("conflict left a device open")
		}
		return
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=^TestRouteConflictReturnsStartupErrorWithoutExiting$")
	cmd.Env = append(os.Environ(), "GOTUN_TEST_ROUTE_CONFLICT=1")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("route conflict terminated the process: %v\n%s", err, output)
	}
}

func nativeWithFakeNetwork(t *testing.T, platform string, cfg *config.Config, run func(string, ...string) ([]byte, error)) (*TunService, *memoryDevice) {
	t.Helper()
	service, err := NewTunService(cfg, logger.NewLogger(false), nil)
	if err != nil {
		t.Fatal(err)
	}
	device := newMemoryDevice()
	service.platform, service.run = platform, run
	service.setupDriver = func() error { return nil }
	service.createDevice = func(string, int) (wgtun.Device, error) { return device, nil }
	service.interfaceByName = func(string) (*net.Interface, error) { return &net.Interface{Index: 37}, nil }
	t.Cleanup(func() { _ = service.Close() })
	return service, device
}

func TestNativeStartRollsBackPartialRoutes(t *testing.T) {
	for _, platform := range []string{"darwin", "linux", "windows"} {
		t.Run(platform, func(t *testing.T) {
			cfg := config.NewConfig()
			cfg.SSHServer = "192.0.2.9:22"
			cfg.TunRoute = []string{"203.0.113.0/24", "198.51.100.0/24"}
			failure := errors.New("route denied")
			var calls []string
			service, device := nativeWithFakeNetwork(t, platform, cfg, func(name string, args ...string) ([]byte, error) {
				call := name + " " + strings.Join(args, " ")
				calls = append(calls, call)
				if strings.Contains(call, "add 198.51.100.0") {
					return nil, failure
				}
				return nil, nil
			})
			if err := service.Start(); !errors.Is(err, failure) {
				t.Fatalf("Start = %v, want route failure; calls=%v", err, calls)
			}
			wantDelete := map[string]string{
				"darwin":  "route delete 203.0.113.0/24 -interface memory-tun",
				"linux":   "ip route del 203.0.113.0/24 dev memory-tun",
				"windows": "route delete 203.0.113.0 mask 255.255.255.0 0.0.0.0 IF 37",
			}[platform]
			if !strings.Contains(strings.Join(calls, "\n"), wantDelete) {
				t.Fatalf("partial route was not removed with its original interface: %v", calls)
			}
			select {
			case <-device.closed:
			default:
				t.Fatal("failed startup left its device open")
			}
		})
	}
}

func TestNativeStartPreservesExistingRoute(t *testing.T) {
	for _, platform := range []string{"darwin", "linux", "windows"} {
		t.Run(platform, func(t *testing.T) {
			cfg := config.NewConfig()
			cfg.TunRoute = []string{"203.0.113.0/24"}
			exists := errors.New("existing user route")
			var calls []string
			service, _ := nativeWithFakeNetwork(t, platform, cfg, func(name string, args ...string) ([]byte, error) {
				call := name + " " + strings.Join(args, " ")
				calls = append(calls, call)
				if strings.Contains(call, "add 203.0.113.0") {
					return []byte("File exists"), exists
				}
				return nil, nil
			})
			if err := service.Start(); !errors.Is(err, exists) {
				t.Fatalf("existing route was reported as ours: %v", err)
			}
			if err := service.Close(); err != nil {
				t.Fatal(err)
			}
			if joined := strings.Join(calls, "\n"); strings.Contains(joined, "delete") || strings.Contains(joined, "route del ") {
				t.Fatalf("existing route was deleted: %s", joined)
			}
		})
	}
}

func TestNativeRouteCleanupFailureCanBeRetried(t *testing.T) {
	for _, platform := range []string{"darwin", "linux", "windows"} {
		t.Run(platform, func(t *testing.T) {
			cfg := config.NewConfig()
			cfg.TunRoute = []string{"203.0.113.0/24", "198.51.100.0/24"}
			failure := errors.New("cleanup denied")
			failCleanup := true
			deletes := map[string]int{}
			service, device := nativeWithFakeNetwork(t, platform, cfg, func(name string, args ...string) ([]byte, error) {
				call := name + " " + strings.Join(args, " ")
				if strings.Contains(call, "delete ") || strings.Contains(call, "route del ") {
					deletes[call]++
					if failCleanup && strings.Contains(call, "203.0.113.0") {
						return nil, failure
					}
				}
				return nil, nil
			})
			if err := service.Start(); err != nil {
				t.Fatal(err)
			}
			if err := service.Close(); !errors.Is(err, failure) {
				t.Fatalf("Close = %v, want cleanup failure", err)
			}
			select {
			case <-device.closed:
				t.Fatal("closed interface before the failed route cleanup could be retried")
			default:
			}
			failCleanup = false
			if err := service.Close(); err != nil {
				t.Fatal(err)
			}
			if err := service.Close(); err != nil {
				t.Fatal(err)
			}
			for command, count := range deletes {
				want := 1
				if strings.Contains(command, "203.0.113.0") {
					want = 2
				}
				if count != want {
					t.Fatalf("cleanup repeated a successful deletion or lost retry: %s called %d times", command, count)
				}
			}
			select {
			case <-device.closed:
			default:
				t.Fatal("successful cleanup left its device open")
			}
		})
	}
}

func TestNativeCloseAcceptsRoutesAlreadyRemovedByOS(t *testing.T) {
	for platform, output := range map[string]string{
		"darwin":  "route: writing to routing socket: not in table",
		"linux":   "RTNETLINK answers: No such process",
		"windows": "The route deletion failed: Element not found.",
	} {
		t.Run(platform, func(t *testing.T) {
			cfg := config.NewConfig()
			cfg.TunRoute = []string{"203.0.113.0/24"}
			service, device := nativeWithFakeNetwork(t, platform, cfg, func(name string, args ...string) ([]byte, error) {
				call := name + " " + strings.Join(args, " ")
				if strings.Contains(call, "delete ") || strings.Contains(call, "route del ") {
					return []byte(output), errors.New("exit status 1")
				}
				return nil, nil
			})
			if err := service.Start(); err != nil {
				t.Fatal(err)
			}
			if err := service.Close(); err != nil {
				t.Fatalf("an absent owned route prevented shutdown: %v", err)
			}
			select {
			case <-device.closed:
			default:
				t.Fatal("absent route left device open")
			}
		})
	}
}

func TestGlobalIPv6AndLoopbackSSHDoNotChangeTheirRoutes(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "2001:db8::9", "fe80::9%en0"} {
		t.Run(ip, func(t *testing.T) {
			cfg := config.NewConfig()
			cfg.TunGlobal = true
			var calls []string
			service, _ := nativeWithFakeNetwork(t, "darwin", cfg, func(name string, args ...string) ([]byte, error) {
				calls = append(calls, name+" "+strings.Join(args, " "))
				return nil, nil
			})
			host, zone, _ := strings.Cut(ip, "%")
			service.ssh = physicalSSH{remote: &net.TCPAddr{IP: net.ParseIP(host), Port: 22, Zone: zone}}
			if err := service.Start(); err != nil {
				t.Fatal(err)
			}
			if err := service.Close(); err != nil {
				t.Fatal(err)
			}
			for _, call := range calls {
				if strings.Contains(call, ip) {
					t.Fatalf("changed unaffected SSH route: %s", call)
				}
			}
		})
	}
}

func TestGlobalFailureRollsBackEveryAppliedRoute(t *testing.T) {
	for _, platform := range []string{"darwin", "linux", "windows"} {
		for _, failedTarget := range []string{"203.0.113.9", "0.0.0.0", "128.0.0.0"} {
			t.Run(platform+"/"+failedTarget, func(t *testing.T) {
				cfg := config.NewConfig()
				cfg.TunGlobal = true
				failure := errors.New("route addition failed")
				applied := map[string]bool{}
				service, device := nativeWithFakeNetwork(t, platform, cfg, func(name string, args ...string) ([]byte, error) {
					call := name + " " + strings.Join(args, " ")
					if strings.Contains(call, " -n get ") {
						return []byte("gateway: 192.0.2.1\ninterface: en0"), nil
					}
					if strings.Contains(call, " -4 route get ") {
						return []byte("203.0.113.9 via 192.0.2.1 dev en0"), nil
					}
					if name == "powershell" {
						return []byte(`{"NextHop":"192.0.2.1","InterfaceIndex":12}`), nil
					}
					for _, target := range []string{"203.0.113.9", "0.0.0.0", "128.0.0.0"} {
						if strings.Contains(call, "add "+target) {
							if target == failedTarget {
								return nil, failure
							}
							applied[target] = true
						}
						if strings.Contains(call, "delete "+target) || strings.Contains(call, "route del "+target) {
							if !applied[target] {
								t.Fatalf("deleted a route not added by us: %s", call)
							}
							delete(applied, target)
						}
					}
					return nil, nil
				})
				service.ssh = physicalSSH{remote: &net.TCPAddr{IP: net.ParseIP("203.0.113.9"), Port: 22}}
				if err := service.Start(); !errors.Is(err, failure) {
					t.Fatalf("Start = %v, want route failure", err)
				}
				if len(applied) != 0 {
					t.Fatalf("startup left routes behind: %v", applied)
				}
				select {
				case <-device.closed:
				default:
					t.Fatal("startup failure left device open")
				}
			})
		}
	}
}

func TestGlobalBypassPreservesDirectlyConnectedInterface(t *testing.T) {
	for _, platform := range []string{"darwin", "linux", "windows"} {
		t.Run(platform, func(t *testing.T) {
			cfg := config.NewConfig()
			cfg.TunGlobal = true
			var calls []string
			service, _ := nativeWithFakeNetwork(t, platform, cfg, func(name string, args ...string) ([]byte, error) {
				call := name + " " + strings.Join(args, " ")
				calls = append(calls, call)
				if strings.Contains(call, " -n get ") {
					return []byte("gateway: link#12\ninterface: en0\n"), nil
				}
				if strings.Contains(call, " -4 route get ") {
					return []byte("203.0.113.9 dev en0 src 203.0.113.10\n"), nil
				}
				if name == "powershell" {
					return []byte(`{"NextHop":"0.0.0.0","InterfaceIndex":12}`), nil
				}
				return nil, nil
			})
			service.ssh = physicalSSH{remote: &net.TCPAddr{IP: net.ParseIP("203.0.113.9"), Port: 22}}
			if err := service.Start(); err != nil {
				t.Fatal(err)
			}
			want := map[string]string{
				"darwin":  "route add 203.0.113.9/32 -interface en0",
				"linux":   "ip route add 203.0.113.9 dev en0",
				"windows": "route add 203.0.113.9 mask 255.255.255.255 0.0.0.0 METRIC 1 IF 12",
			}[platform]
			if !strings.Contains(strings.Join(calls, "\n"), want) {
				t.Fatalf("lost on-link SSH interface: %v", calls)
			}
		})
	}
}

func (d *retryCloseDevice) Close() error {
	d.closes++
	if d.failure != nil {
		return d.failure
	}
	return d.memoryDevice.Close()
}

func TestNativeCloseReportsDeviceFailureAndRetries(t *testing.T) {
	failure := errors.New("device close failed")
	device := &retryCloseDevice{memoryDevice: newMemoryDevice(), failure: failure}
	service := &TunService{dev: device}
	if err := service.Close(); !errors.Is(err, failure) {
		t.Fatalf("Close = %v, want device failure", err)
	}
	device.failure = nil
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	if device.closes != 2 {
		t.Fatalf("device closed %d times, want one failure and one successful retry", device.closes)
	}
}

func TestMacRouteExitZeroFailureCannotOwnOrDeleteExistingRoute(t *testing.T) {
	for _, message := range []string{"File exists", "Network is unreachable", "Permission denied", "Operation not permitted"} {
		t.Run(message, func(t *testing.T) {
			cfg := config.NewConfig()
			cfg.TunRoute = []string{"203.0.113.0/24"}
			var deletes int
			service, _ := nativeWithFakeNetwork(t, "darwin", cfg, func(name string, args ...string) ([]byte, error) {
				if name == "route" && args[0] == "add" {
					return []byte("route: writing to routing socket: " + message + "\nadd net 203.0.113.0: " + message + "\n"), nil
				}
				if name == "route" && args[0] == "delete" {
					deletes++
				}
				return nil, nil
			})
			if err := service.Start(); err == nil {
				t.Fatal("macOS route failure exit 0 was treated as successful ownership")
			}
			if err := service.Close(); err != nil {
				t.Fatal(err)
			}
			if deletes != 0 {
				t.Fatal("deleted a route after a failed add")
			}
		})
	}
}

func TestMacDeleteExitZeroFailureIsRetried(t *testing.T) {
	cfg := config.NewConfig()
	cfg.TunRoute = []string{"203.0.113.0/24"}
	failed := true
	deletes := 0
	service, device := nativeWithFakeNetwork(t, "darwin", cfg, func(name string, args ...string) ([]byte, error) {
		if name == "route" && args[0] == "delete" {
			deletes++
			if failed {
				return []byte("route: writing to routing socket: Permission denied\n"), nil
			}
		}
		return nil, nil
	})
	if err := service.Start(); err != nil {
		t.Fatal(err)
	}
	if err := service.Close(); err == nil {
		t.Fatal("failed deletion lost retry state")
	}
	select {
	case <-device.closed:
		t.Fatal("closed device while route restoration failed")
	default:
	}
	failed = false
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	if deletes != 2 {
		t.Fatalf("failed deletion retried %d times", deletes)
	}
}
