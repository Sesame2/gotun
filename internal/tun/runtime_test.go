package tun

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Sesame2/gotun/internal/config"
	"github.com/Sesame2/gotun/internal/logger"
	wgtun "golang.zx2c4.com/wireguard/tun"
	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
)

// memoryDevice never opens a network interface or changes the host network.
type memoryDevice struct {
	reads  chan []byte
	writes chan []byte
	closed chan struct{}
	events chan wgtun.Event
	once   sync.Once
}

func newMemoryDevice() *memoryDevice {
	return &memoryDevice{
		reads:  make(chan []byte, 1),
		writes: make(chan []byte, 1),
		closed: make(chan struct{}),
		events: make(chan wgtun.Event),
	}
}

func (d *memoryDevice) File() *os.File             { return nil }
func (d *memoryDevice) MTU() (int, error)          { return 1500, nil }
func (d *memoryDevice) Name() (string, error)      { return "memory-tun", nil }
func (d *memoryDevice) Events() <-chan wgtun.Event { return d.events }
func (d *memoryDevice) BatchSize() int             { return 1 }
func (d *memoryDevice) Close() error {
	d.once.Do(func() { close(d.closed); close(d.events) })
	return nil
}

func (d *memoryDevice) Read(bufs [][]byte, sizes []int, offset int) (int, error) {
	select {
	case data, ok := <-d.reads:
		if !ok {
			return 0, net.ErrClosed
		}
		sizes[0] = copy(bufs[0][offset:], data)
		return 1, nil
	case <-d.closed:
		return 0, net.ErrClosed
	}
}

func (d *memoryDevice) Write(bufs [][]byte, offset int) (int, error) {
	select {
	case <-d.closed:
		return 0, net.ErrClosed
	default:
	}
	for _, data := range bufs {
		select {
		case d.writes <- bytes.Clone(data[offset:]):
		case <-d.closed:
			return 0, net.ErrClosed
		}
	}
	return len(bufs), nil
}

func enqueuePacket(endpoint *channel.Endpoint, payload []byte) tcpip.Error {
	packet := stack.NewPacketBuffer(stack.PacketBufferOptions{Payload: buffer.MakeWithData(payload)})
	defer packet.DecRef()
	var packets stack.PacketBufferList
	packets.PushBack(packet)
	_, err := endpoint.WritePackets(packets)
	return err
}

func TestOutputPumpForwardsQueuedPacketAndStopsOnClose(t *testing.T) {
	for _, hasPacket := range []bool{false, true} {
		name := "empty queue"
		if hasPacket {
			name = "queued packet"
		}
		t.Run(name, func(t *testing.T) {
			device := newMemoryDevice()
			endpoint := channel.New(2, 1500, "")
			service := &TunService{dev: device, endpoint: endpoint, logger: logger.NewLogger(false)}
			payload := []byte{0x45, 0, 0, 8, 0x10, 0x20, 0x30, 0x40}
			if hasPacket {
				if err := enqueuePacket(endpoint, payload); err != nil {
					t.Fatal(err)
				}
			}
			done := make(chan struct{})
			go func() { service.pumpStackToTun(); close(done) }()
			t.Cleanup(func() {
				service.Close()
				// Give a pump that failed to stop a final write against the closed device.
				_ = enqueuePacket(endpoint, payload)
				select {
				case <-done:
				case <-time.After(time.Second):
					t.Error("output pump did not stop during cleanup")
				}
				endpoint.Close()
			})
			if hasPacket {
				select {
				case got := <-device.writes:
					if !bytes.Equal(got, payload) {
						t.Fatalf("forwarded payload = %x, want %x", got, payload)
					}
				case <-time.After(time.Second):
					t.Fatal("queued packet was not forwarded")
				}
			}
			if err := service.Close(); err != nil {
				t.Fatal(err)
			}
			if err := service.Close(); err != nil {
				t.Fatal(err)
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("Close left the empty output pump running")
			}
		})
	}
}

type retainingDispatcher struct{ packets chan *stack.PacketBuffer }

func (d retainingDispatcher) DeliverNetworkPacket(_ tcpip.NetworkProtocolNumber, packet *stack.PacketBuffer) {
	d.packets <- packet.IncRef()
}
func (retainingDispatcher) DeliverLinkPacket(tcpip.NetworkProtocolNumber, *stack.PacketBuffer) {}

func TestInputPumpReleasesItsPacketReference(t *testing.T) {
	device := newMemoryDevice()
	device.reads <- []byte{0x45, 0, 0, 4}
	close(device.reads)
	endpoint := channel.New(1, 1500, "")
	defer endpoint.Close()
	dispatcher := retainingDispatcher{packets: make(chan *stack.PacketBuffer, 1)}
	endpoint.Attach(dispatcher)
	service := &TunService{dev: device, endpoint: endpoint, logger: logger.NewLogger(false)}
	done := make(chan struct{})
	go func() { service.pumpTunToStack(); close(done) }()
	defer service.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("input pump did not stop on closed device")
	}
	select {
	case packet := <-dispatcher.packets:
		defer packet.DecRef()
		if got := packet.ReadRefs(); got != 1 {
			t.Fatalf("packet retains %d references, want only the dispatcher's reference", got)
		}
	default:
		t.Fatal("input packet was not delivered")
	}
}

func TestNetstackUsesConfiguredPrefix(t *testing.T) {
	for _, cidr := range []string{"10.1.2.3/8", "172.16.4.8/16", "192.0.2.5/30", "192.0.2.5/32"} {
		t.Run(cidr, func(t *testing.T) {
			cfg := config.NewConfig()
			cfg.TunCIDR = cidr
			service, err := NewTunService(cfg, logger.NewLogger(false), nil)
			if err != nil {
				t.Fatal(err)
			}
			service.initNetstack()
			defer func() {
				service.Close()
				service.stack.Wait()
			}()
			ip, network, err := net.ParseCIDR(cidr)
			if err != nil {
				t.Fatal(err)
			}
			prefix, _ := network.Mask.Size()
			address, addrErr := service.stack.GetMainNICAddress(1, ipv4.ProtocolNumber)
			if addrErr != nil {
				t.Fatal(addrErr)
			}
			if address.Address.String() != ip.String() || address.PrefixLen != prefix {
				t.Fatalf("netstack address = %s, want %s/%d", address, ip, prefix)
			}
		})
	}
}

func TestSetupTunIPUsesConfiguredPrefixOnLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux command path")
	}
	dir := t.TempDir()
	logPath := filepath.Join(dir, "commands")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$GOTUN_TUN_COMMAND_LOG\"\n"
	if err := os.WriteFile(filepath.Join(dir, "ip"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("GOTUN_TUN_COMMAND_LOG", logPath)
	cfg := config.NewConfig()
	cfg.TunCIDR = "172.16.4.8/16"
	service, err := NewTunService(cfg, logger.NewLogger(false), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.setupTunIP("memory-tun"); err != nil {
		t.Fatal(err)
	}
	commands, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(string(commands)), "addr add 172.16.4.8/16 dev memory-tun\nlink set memory-tun up"; got != want {
		t.Fatalf("commands = %q, want %q", got, want)
	}
}
