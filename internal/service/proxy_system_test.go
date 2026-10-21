package service

import (
	"errors"
	"fmt"
	"github.com/Sesame2/gotun/internal/logger"
	"github.com/Sesame2/gotun/internal/testutil"
	"net"
	"strings"
	"sync"
	"testing"
)

type fakeSystemProxy struct {
	disableFailure            error
	mu                        sync.Mutex
	enabled                   bool
	failure                   error
	clears, enables, disables int
}

func (m *fakeSystemProxy) Enable() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.enables++
	if m.failure != nil {
		return m.failure
	}
	m.enabled = true
	return nil
}
func (m *fakeSystemProxy) Disable() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.disables++
	if m.disableFailure != nil {
		return m.disableFailure
	}
	if m.failure != nil {
		return m.failure
	}
	m.enabled = false
	return nil
}
func (m *fakeSystemProxy) Clear() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clears++
	if m.failure != nil {
		return m.failure
	}
	m.enabled = false
	return nil
}
func (m *fakeSystemProxy) IsEnabled() bool { m.mu.Lock(); defer m.mu.Unlock(); return m.enabled }

func TestSystemProxyControlAndActualStatus(t *testing.T) {
	s := NewProxyService()
	fake := &fakeSystemProxy{}
	s.proxyMgr = fake
	s.cfg.SystemProxy = true
	if s.GetStatus().SystemProxy {
		t.Fatal("preference reported as actual proxy status")
	}
	if s.SetSystemProxy(true) == nil {
		t.Fatal("enabled proxy with stopped service")
	}
	if err := s.SetSystemProxy(false); err != nil {
		t.Fatal(err)
	}
	s.status = StatusRunning
	if err := s.SetSystemProxy(true); err != nil {
		t.Fatal(err)
	}
	if !s.GetStatus().SystemProxy {
		t.Fatal("enabled proxy not reflected")
	}
	fake.failure = errors.New("permission denied")
	if s.ClearSystemProxy() == nil || !s.GetStatus().SystemProxy {
		t.Fatal("failed clear lost state or error")
	}
	fake.failure = nil
	if err := s.ClearSystemProxy(); err != nil {
		t.Fatal(err)
	}
	if fake.clears != 2 || s.GetStatus().SystemProxy {
		t.Fatal("clear did not update state")
	}
	s.cfg.SystemProxy = false
	if err := s.SetSystemProxy(true); err != nil {
		t.Fatal(err)
	}
	if err := s.Stop(); err != nil {
		t.Fatal(err)
	}
	if fake.enabled {
		t.Fatal("Stop left dynamically enabled proxy active")
	}
}

func TestSystemProxyOperationsSerialize(t *testing.T) {
	s := NewProxyService()
	s.status = StatusRunning
	s.proxyMgr = &fakeSystemProxy{}
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = s.SetSystemProxy(true); _ = s.GetStatus(); _ = s.ClearSystemProxy() }()
	}
	wg.Wait()
	s = NewProxyService()
	if err := s.SetSystemProxy(false); err != nil {
		t.Fatal(err)
	}
}

func TestSystemProxyStopFailureCanBeRetried(t *testing.T) {
	s := NewProxyService()
	fake := &fakeSystemProxy{enabled: true, failure: errors.New("denied")}
	s.proxyMgr = fake
	s.status = StatusRunning
	if s.Stop() == nil {
		t.Fatal("Stop suppressed OS cleanup failure")
	}
	if s.GetStatus().Status != StatusStopped || !s.GetStatus().SystemProxy {
		t.Fatal("cleanup failure should preserve pending system proxy state")
	}
	fake.failure = nil
	if err := s.Stop(); err != nil {
		t.Fatal(err)
	}
	if fake.enabled || fake.disables != 2 {
		t.Fatal("Stop did not retry pending cleanup")
	}
}

func TestAutomaticSystemProxyFailureFailsStartAndCleansResources(t *testing.T) {
	for _, cleanupFails := range []bool{false, true} {
		t.Run(fmt.Sprint(cleanupFails), func(t *testing.T) {
			server := testutil.NewSSHServer(t)
			cfg := server.Config()
			cfg.SystemProxy = true
			cfg.ListenAddr = testutil.AvailableAddr(t)
			cfg.SocksAddr = testutil.AvailableAddr(t)
			failure := errors.New("system proxy permission denied")
			cleanupFailure := errors.New("cleanup denied")
			fake := &fakeSystemProxy{failure: failure, enabled: cleanupFails}
			if cleanupFails {
				fake.disableFailure = cleanupFailure
			}
			s := NewProxyService()
			s.newSystemProxy = func(*logger.Logger, string, string) systemProxyManager { return fake }
			s.SetConfig(cfg)
			err := s.Start()
			if !errors.Is(err, failure) || !strings.Contains(err.Error(), "设置系统代理失败") {
				t.Fatalf("Start error=%v", err)
			}
			if cleanupFails && !errors.Is(err, cleanupFailure) {
				t.Fatalf("cleanup error missing: %v", err)
			}
			if s.GetStatus().Status != StatusError || s.sshClient != nil || s.httpProxy != nil || s.socksProxy != nil {
				t.Fatal("automatic proxy failure leaked running resources")
			}
			if s.GetStatus().ErrorMessage != err.Error() {
				t.Fatal("status omitted startup failure")
			}
			if s.GetStatus().SystemProxy != cleanupFails {
				t.Fatal("wrong cleanup state")
			}
			// HTTP and SOCKS listeners must both be released after startup fails.
			for _, addr := range []string{cfg.ListenAddr, cfg.SocksAddr} {
				listener, err := net.Listen("tcp", addr)
				if err != nil {
					t.Fatal(err)
				}
				listener.Close()
			}
			fake.failure = nil
			fake.disableFailure = nil
			if err := s.Stop(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRestartCannotLosePendingSystemProxyCleanup(t *testing.T) {
	server := testutil.NewSSHServer(t)
	cfg := server.Config()
	cfg.SystemProxy = false
	pendingFailure := errors.New("pending cleanup denied")
	pending := &fakeSystemProxy{enabled: true, disableFailure: pendingFailure}
	next := &fakeSystemProxy{}
	s := NewProxyService()
	s.status = StatusRunning
	s.proxyMgr = pending
	creations := 0
	s.newSystemProxy = func(*logger.Logger, string, string) systemProxyManager { creations++; return next }
	t.Cleanup(func() { pending.disableFailure = nil; s.Stop() })
	if err := s.Stop(); !errors.Is(err, pendingFailure) {
		t.Fatalf("Stop error=%v", err)
	}
	s.SetConfig(cfg)
	err := s.Start()
	if !errors.Is(err, pendingFailure) {
		t.Fatalf("Start lost pending cleanup error: %v", err)
	}
	if creations != 0 || s.proxyMgr != pending || s.sshClient != nil || s.httpProxy != nil || s.GetStatus().Status != StatusError || !s.GetStatus().SystemProxy {
		t.Fatal("Start replaced pending proxy manager or started resources")
	}
	pending.disableFailure = nil
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	if creations != 1 || pending.enabled || s.proxyMgr != next || s.GetStatus().SystemProxy {
		t.Fatal("successful cleanup did not permit manual restart")
	}
}
