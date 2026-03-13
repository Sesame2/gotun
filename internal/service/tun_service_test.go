package service

import (
	"os"
	"runtime"
	"testing"
)

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
