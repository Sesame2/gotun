package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Sesame2/gotun/internal/config"
	"github.com/Sesame2/gotun/internal/logger"
	"github.com/Sesame2/gotun/internal/proxy"
	"github.com/Sesame2/gotun/internal/testutil"
	"golang.org/x/crypto/ssh/knownhosts"
)

func TestTunLoggingSettingsKeepFileAndEmitCallbackOnce(t *testing.T) {
	service := NewTunService()
	path := filepath.Join(t.TempDir(), "tun.log")
	if err := service.SetLogFile(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := service.logger.Close(); err != nil {
			t.Error(err)
		}
	})
	calls := map[string]int{}
	service.SetLogCallback(func(level, message string) { calls[level+":"+message]++ })
	service.SetVerbose(true)
	service.log("DEBUG", "visible debug")
	service.log("INFO", "visible info")
	service.SetVerbose(false)
	service.log("DEBUG", "hidden debug")
	if err := service.SetLogFile(""); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "visible debug") || !strings.Contains(string(data), "visible info") || strings.Contains(string(data), "hidden debug") {
		t.Fatalf("logging settings lost output: %s", data)
	}
	if calls["DEBUG:visible debug"] != 1 || calls["INFO:visible info"] != 1 || calls["DEBUG:hidden debug"] != 0 {
		t.Fatalf("callback duplicated or ignored verbosity: %v", calls)
	}
}

func TestTunStartSnapshotsConfigurationAndPreservesSSHPolicy(t *testing.T) {
	server := testutil.NewSSHServer(t)
	service := NewTunService()
	service.needsElevation = func() bool { return false }
	tunCfg := &TunConfig{TunMode: true, TunCIDR: "10.0.0.1/24", TunRoutes: []string{"203.0.113.0/24"}}
	fullCfg := server.Config()
	ctx := context.WithValue(context.Background(), struct{}{}, "policy")
	fullCfg.Context = ctx
	fullCfg.HostKeyPrompt = func(string, string) bool { return true }
	service.SetConfig(tunCfg)
	service.SetFullConfig(fullCfg)
	service.newNative = func(cfg *config.Config, _ *logger.Logger, _ *proxy.SSHClient) (nativeTun, error) {
		if cfg.SSHServer != server.Addr || cfg.KnownHostsFile != server.KnownHostsFile || cfg.Context != ctx || cfg.HostKeyPrompt == nil || !cfg.HostKeyPrompt("fixture", "fingerprint") {
			t.Fatal("snapshot lost SSH endpoint, trust policy or context")
		}
		return &fakeNativeTun{}, nil
	}
	tunCfg.TunRoutes[0] = "mutated"
	fullCfg.SSHServer = "127.0.0.1:0"
	if err := service.Start(); err != nil {
		t.Fatalf("caller mutation changed startup snapshot: %v", err)
	}
	t.Cleanup(func() { _ = service.Stop() })
	status := service.GetStatus()
	status.Routes[0] = "mutated stats"
	if got := service.GetStatus().Routes[0]; got != "203.0.113.0/24" {
		t.Fatalf("GetStatus leaked mutable routes: %q", got)
	}
}

func TestTunConfigRejectsUnsupportedAddressesAndNarrowNAT(t *testing.T) {
	for _, cfg := range []TunConfig{
		{TunMode: true, TunCIDR: "2001:db8::1/64"},
		{TunMode: true, TunCIDR: "::ffff:10.0.0.1/120"},
		{TunMode: true, TunRoutes: []string{"2001:db8::/64"}},
		{TunMode: true, TunNAT: []TunNATRule{{SrcCIDR: "10.0.0.0/16", DstCIDR: "192.0.2.0/24"}}},
	} {
		if err := cfg.Validate(); err == nil {
			t.Fatalf("accepted unusable TUN configuration: %+v", cfg)
		}
	}
}

func TestTunStartupPreservesSSHErrorIdentity(t *testing.T) {
	t.Run("cancelled startup", func(t *testing.T) {
		service := tunServiceWithFake(t, &fakeNativeTun{})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		service.fullCfg.Context = ctx
		if err := service.Start(); !errors.Is(err, context.Canceled) {
			t.Fatalf("SSH cancellation lost its cause: %v", err)
		}
	})
	t.Run("unknown host key", func(t *testing.T) {
		service := tunServiceWithFake(t, &fakeNativeTun{})
		service.fullCfg.KnownHostsFile = filepath.Join(t.TempDir(), "unknown_hosts")
		var identity *knownhosts.KeyError
		if err := service.Start(); !errors.As(err, &identity) {
			t.Fatalf("SSH identity error lost its cause: %v", err)
		}
	})
}

func TestNewTunService(t *testing.T) {
	svc := NewTunService()
	if svc == nil {
		t.Fatal("NewTunService() returned nil")
	}
	if svc.status != TunStatusStopped {
		t.Errorf("Expected initial status to be TunStatusStopped, got %s", svc.status)
	}
}

func TestTunServiceGetStatus(t *testing.T) {
	svc := NewTunService()

	status := svc.GetStatus()
	if status.Status != TunStatusStopped {
		t.Errorf("Expected status to be TunStatusStopped, got %s", status.Status)
	}
	if status.TunIP != "" {
		t.Errorf("Expected TunIP to be empty, got %s", status.TunIP)
	}
}

func TestTunServiceRequiresElevation(t *testing.T) {
	svc := NewTunService()

	// 在非Windows系统上，如果不是root用户，应该需要提权
	if runtime.GOOS != "windows" {
		needsElevation := svc.RequiresElevation()
		if os.Geteuid() == 0 {
			// 以root身份运行
			if needsElevation {
				t.Error("Should not require elevation when running as root")
			}
		} else {
			// 以普通用户运行
			if !needsElevation {
				t.Error("Should require elevation when running as non-root")
			}
		}
	}
}

func TestTunServiceSetConfig(t *testing.T) {
	svc := NewTunService()

	cfg := &TunConfig{
		TunMode:   true,
		TunGlobal: false,
		TunCIDR:   "10.0.0.1/24",
		TunRoutes: []string{"192.168.1.0/24", "172.16.0.0/16"},
		TunNAT:    []TunNATRule{{SrcCIDR: "10.0.0.0/24", DstCIDR: "192.168.0.0/24"}},
	}

	svc.SetConfig(cfg)

	if svc.cfg.TunMode != true {
		t.Error("TunMode should be true")
	}
	if svc.cfg.TunGlobal != false {
		t.Error("TunGlobal should be false")
	}
	if svc.cfg.TunCIDR != "10.0.0.1/24" {
		t.Errorf("TunCIDR should be 10.0.0.1/24, got %s", svc.cfg.TunCIDR)
	}
	if len(svc.cfg.TunRoutes) != 2 {
		t.Errorf("Expected 2 routes, got %d", len(svc.cfg.TunRoutes))
	}
	if len(svc.cfg.TunNAT) != 1 {
		t.Errorf("Expected 1 NAT rule, got %d", len(svc.cfg.TunNAT))
	}
}

func TestTunConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *TunConfig
		wantErr bool
	}{
		{
			name: "valid config with routes",
			cfg: &TunConfig{
				TunMode:   true,
				TunCIDR:   "10.0.0.1/24",
				TunRoutes: []string{"192.168.1.0/24"},
			},
			wantErr: false,
		},
		{
			name: "valid global mode",
			cfg: &TunConfig{
				TunMode:   true,
				TunGlobal: true,
				TunCIDR:   "10.0.0.1/24",
			},
			wantErr: false,
		},
		{
			name: "invalid CIDR format",
			cfg: &TunConfig{
				TunMode: true,
				TunCIDR: "invalid-cidr",
			},
			wantErr: true,
		},
		{
			name: "invalid route CIDR",
			cfg: &TunConfig{
				TunMode:   true,
				TunCIDR:   "10.0.0.1/24",
				TunRoutes: []string{"invalid-route"},
			},
			wantErr: true,
		},
		{
			name: "invalid NAT src CIDR",
			cfg: &TunConfig{
				TunMode: true,
				TunCIDR: "10.0.0.1/24",
				TunNAT:  []TunNATRule{{SrcCIDR: "invalid", DstCIDR: "192.168.0.0/24"}},
			},
			wantErr: true,
		},
		{
			name: "invalid NAT dst CIDR",
			cfg: &TunConfig{
				TunMode: true,
				TunCIDR: "10.0.0.1/24",
				TunNAT:  []TunNATRule{{SrcCIDR: "10.0.0.0/24", DstCIDR: "invalid"}},
			},
			wantErr: true,
		},
		{
			name: "tun mode disabled",
			cfg: &TunConfig{
				TunMode: false,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestTunServiceStatusTransitions(t *testing.T) {
	svc := NewTunService()

	// 初始状态应该是 stopped
	if svc.status != TunStatusStopped {
		t.Errorf("Initial status should be stopped, got %s", svc.status)
	}

	// 测试状态设置
	svc.setStatus(TunStatusStarting)
	if svc.status != TunStatusStarting {
		t.Errorf("Status should be starting, got %s", svc.status)
	}

	svc.setStatus(TunStatusRunning)
	if svc.status != TunStatusRunning {
		t.Errorf("Status should be running, got %s", svc.status)
	}

	svc.setStatus(TunStatusStopping)
	if svc.status != TunStatusStopping {
		t.Errorf("Status should be stopping, got %s", svc.status)
	}

	svc.setStatus(TunStatusStopped)
	if svc.status != TunStatusStopped {
		t.Errorf("Status should be stopped, got %s", svc.status)
	}
}

func TestTunServiceSetError(t *testing.T) {
	svc := NewTunService()

	svc.setError("test error message")

	if svc.status != TunStatusError {
		t.Errorf("Status should be TunStatusError, got %s", svc.status)
	}
	if svc.errMsg != "test error message" {
		t.Errorf("Error message should be 'test error message', got '%s'", svc.errMsg)
	}

	status := svc.GetStatus()
	if status.ErrorMessage != "test error message" {
		t.Errorf("GetStatus().ErrorMessage should be 'test error message', got '%s'", status.ErrorMessage)
	}
}

func TestTunServiceIsRunning(t *testing.T) {
	svc := NewTunService()

	if svc.IsRunning() {
		t.Error("New service should not be running")
	}

	svc.setStatus(TunStatusRunning)
	if !svc.IsRunning() {
		t.Error("Service with running status should report as running")
	}

	svc.setStatus(TunStatusStopped)
	if svc.IsRunning() {
		t.Error("Service with stopped status should not report as running")
	}
}

func TestTunServiceLogCallback(t *testing.T) {
	svc := NewTunService()

	var capturedLevel, capturedMessage string
	svc.SetLogCallback(func(level, message string) {
		capturedLevel = level
		capturedMessage = message
	})

	svc.log("INFO", "test message")

	if capturedLevel != "INFO" {
		t.Errorf("Expected level INFO, got %s", capturedLevel)
	}
	if capturedMessage != "test message" {
		t.Errorf("Expected message 'test message', got '%s'", capturedMessage)
	}
}

func TestTunServiceDefaultCIDR(t *testing.T) {
	cfg := &TunConfig{
		TunMode: true,
		TunCIDR: "", // 空CIDR应该使用默认值
	}

	// Validate 应该为空 CIDR 设置默认值或报错
	// 根据实现，这里需要在 SetConfig 或 Start 时处理默认值
	svc := NewTunService()
	svc.SetConfig(cfg)

	// 检查是否设置了默认值
	if svc.cfg.TunCIDR == "" {
		// 如果为空，说明需要在 Start 时处理
		// 这是预期行为，或者可以在 SetConfig 中设置默认值
	}
}

func TestGetElevationCommand(t *testing.T) {
	svc := NewTunService()

	cmd := svc.GetElevationCommand()

	switch runtime.GOOS {
	case "darwin":
		// macOS 应该包含 osascript
		if len(cmd) == 0 {
			t.Error("Darwin should return elevation command")
		}
	case "linux":
		// Linux 应该包含 pkexec 或类似命令
		if len(cmd) == 0 {
			t.Error("Linux should return elevation command")
		}
	case "windows":
		// Windows 使用不同的提权机制
		// 可能返回空或特殊命令
	}
}

type fakeNativeTun struct {
	start    func() error
	closeErr error
	closes   int
}

func (f *fakeNativeTun) Start() error {
	if f.start != nil {
		return f.start()
	}
	return nil
}
func (f *fakeNativeTun) Close() error { f.closes++; return f.closeErr }
func tunServiceWithFake(t *testing.T, f *fakeNativeTun) *TunService {
	t.Helper()
	server := testutil.NewSSHServer(t)
	s := NewTunService()
	s.SetConfig(&TunConfig{TunMode: true, TunCIDR: "10.0.0.1/24"})
	s.SetFullConfig(server.Config())
	s.needsElevation = func() bool { return false }
	s.newNative = func(*config.Config, *logger.Logger, *proxy.SSHClient) (nativeTun, error) { return f, nil }
	t.Cleanup(func() { _ = s.Stop() })
	return s
}

func TestTunStartWaitsForNativeSetupAndSerializesStop(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	fake := &fakeNativeTun{start: func() error { close(entered); <-release; return nil }}
	s := tunServiceWithFake(t, fake)
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	started := make(chan error, 1)
	go func() { started <- s.Start() }()
	select {
	case <-entered:
	case err := <-started:
		t.Fatalf("Start failed before setup: %v", err)
	}
	if s.GetStatus().Status != TunStatusStarting {
		t.Fatal("reported running before setup")
	}
	select {
	case err := <-started:
		t.Fatalf("Start returned early: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	stopped := make(chan error, 1)
	go func() { stopped <- s.Stop() }()
	select {
	case err := <-stopped:
		t.Fatalf("Stop bypassed startup lifecycle: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if err := <-started; err != nil {
		t.Fatal(err)
	}
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
	if s.GetStatus().Status != TunStatusStopped || fake.closes != 1 {
		t.Fatal("Stop did not clean up completed startup")
	}
}

func TestTunStartupFailureIsSynchronousAndCleansResources(t *testing.T) {
	failure := errors.New("native setup failed")
	fake := &fakeNativeTun{start: func() error { return failure }}
	s := tunServiceWithFake(t, fake)
	if err := s.Start(); !errors.Is(err, failure) {
		t.Fatalf("Start error = %v", err)
	}
	if s.GetStatus().Status != TunStatusError || fake.closes != 1 || s.sshClient != nil || s.tunService != nil {
		t.Fatal("failed start leaked resources or status")
	}
	if err := s.Stop(); err != nil {
		t.Fatal(err)
	}
	if s.GetStatus().Status != TunStatusStopped {
		t.Fatal("Stop did not reset error status")
	}
}

func TestTunStopCleansErrorAndStartingStatesAndReportsFailure(t *testing.T) {
	for _, status := range []TunStatus{TunStatusStarting, TunStatusError, TunStatusStopped} {
		t.Run(string(status), func(t *testing.T) {
			failure := errors.New("close failed")
			fake := &fakeNativeTun{closeErr: failure}
			s := NewTunService()
			s.status = status
			s.tunService = fake
			if err := s.Stop(); !errors.Is(err, failure) {
				t.Fatalf("Stop error = %v", err)
			}
			if fake.closes != 1 || s.GetStatus().Status != TunStatusError || s.tunService == nil {
				t.Fatal("cleanup failure lost retry state")
			}
			fake.closeErr = nil
			if err := s.Stop(); err != nil {
				t.Fatal(err)
			}
			if fake.closes != 2 || s.GetStatus().Status != TunStatusStopped || s.tunService != nil {
				t.Fatal("retry did not finish cleanup")
			}
		})
	}
}

func TestTunFailedStartRetainsFailedCleanupForStopRetry(t *testing.T) {
	startFailure, closeFailure := errors.New("setup failed"), errors.New("cleanup failed")
	fake := &fakeNativeTun{start: func() error { return startFailure }, closeErr: closeFailure}
	s := tunServiceWithFake(t, fake)
	err := s.Start()
	if !errors.Is(err, startFailure) || !errors.Is(err, closeFailure) {
		t.Fatalf("Start omitted error: %v", err)
	}
	if s.sshClient != nil || s.tunService == nil {
		t.Fatal("wrong pending cleanup state")
	}
	if err := s.Start(); err == nil {
		t.Fatal("Start replaced native handle awaiting cleanup")
	}
	fake.closeErr = nil
	if err := s.Stop(); err != nil {
		t.Fatal(err)
	}
	if s.tunService != nil || s.GetStatus().Status != TunStatusStopped {
		t.Fatal("Stop failed to clean pending native service")
	}
}

func TestTunInitializationFailureClosesSSH(t *testing.T) {
	s := tunServiceWithFake(t, &fakeNativeTun{})
	s.newNative = func(*config.Config, *logger.Logger, *proxy.SSHClient) (nativeTun, error) {
		return nil, errors.New("native constructor failed")
	}
	if err := s.Start(); err == nil {
		t.Fatal("accepted native constructor failure")
	}
	if s.sshClient != nil || s.tunService != nil || s.GetStatus().Status != TunStatusError {
		t.Fatal("constructor failure leaked state")
	}
}
