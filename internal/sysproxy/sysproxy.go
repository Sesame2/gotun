package sysproxy

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/Sesame2/gotun/internal/logger"
	"github.com/Sesame2/gotun/internal/utils"
)

type Manager struct {
	mu                  sync.Mutex
	platform            string
	run                 func(string, ...string) ([]byte, error)
	logger              *logger.Logger
	httpAddr, socksAddr string
	enabled             bool
	restore             func() error
	clearing            bool
	macBaseline         macSettings
	acquireLease        func() (func(), error)
	releaseLease        func()
}

// NewManager 创建新的系统代理管理器
func NewManager(log *logger.Logger, httpListenAddr, socksListenAddr string) *Manager {
	// 解析地址，确保格式正确
	host, portStr, err := net.SplitHostPort(httpListenAddr)
	if err != nil {
		log.Warnf("无法解析监听地址 %s: %v, 使用原始地址", httpListenAddr, err)
		host = "127.0.0.1"
		portStr = "8080"
	}

	// 如果主机为空，使用localhost
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	httpAddr := net.JoinHostPort(host, portStr)

	// 解析 socksAddr
	var finalSocksAddr string
	if socksListenAddr != "" {
		shost, sportStr, serr := net.SplitHostPort(socksListenAddr)
		if serr == nil {
			if shost == "" || shost == "0.0.0.0" || shost == "::" {
				shost = "127.0.0.1"
			}
			finalSocksAddr = net.JoinHostPort(shost, sportStr)
		}
	}

	return &Manager{
		logger:       log,
		platform:     runtime.GOOS,
		run:          func(name string, args ...string) ([]byte, error) { return exec.Command(name, args...).CombinedOutput() },
		httpAddr:     httpAddr,
		socksAddr:    finalSocksAddr,
		acquireLease: acquireSystemProxyLease,
	}
}

// Enable snapshots before writing; a failed rollback remains retryable.
func (m *Manager) Enable() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.enabled && !m.clearing {
		return nil
	}
	if err := m.cleanupLocked(); err != nil {
		return err
	}
	if err := m.ensureLeaseLocked(); err != nil {
		return err
	}
	restore, err := m.snapshot()
	if err != nil {
		m.releaseLeaseLocked()
		return fmt.Errorf("保存当前代理设置失败: %w", err)
	}
	m.restore = restore
	switch m.platform {
	case "darwin":
		err = m.enableMacOS()
	case "windows":
		err = m.enableWindows()
	case "linux":
		err = m.enableLinux()
	}
	if err != nil {
		return errors.Join(err, m.cleanupLocked())
	}
	m.enabled = true
	m.logger.Info("系统代理已启用")
	return nil
}

// Disable restores only the settings owned by this instance.
func (m *Manager) Disable() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cleanupLocked()
}

// Clear deliberately replaces any saved restore intent, even for an idle manager.
func (m *Manager) Clear() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ensureLeaseLocked(); err != nil {
		return err
	}
	m.restore = nil
	m.clearing = true
	return m.cleanupLocked()
}

func (m *Manager) cleanupLocked() error {
	var err error
	if m.clearing {
		switch m.platform {
		case "darwin":
			err = m.clearMacOS()
		case "windows":
			err = m.clearWindows()
		case "linux":
			err = m.clearLinux()
		default:
			err = fmt.Errorf("不支持的操作系统: %s", m.platform)
		}
	} else if m.restore != nil {
		err = m.restore()
	}
	if err != nil {
		return err
	}
	m.restore = nil
	m.clearing = false
	m.enabled = false
	m.releaseLeaseLocked()
	return nil
}

func acquireSystemProxyLease() (func(), error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(home, ".gotun")
	if err := utils.EnsurePrivateDir(dir, home); err != nil {
		return nil, err
	}
	return utils.LockFile(filepath.Join(dir, "system-proxy.lock"), true, home)
}

func (m *Manager) ensureLeaseLocked() error {
	if m.releaseLease != nil {
		return nil
	}
	release, err := m.acquireLease()
	if errors.Is(err, utils.ErrFileLocked) {
		return fmt.Errorf("另一实例正在管理系统代理: %w", err)
	}
	if err != nil {
		return fmt.Errorf("锁定系统代理配置失败: %w", err)
	}
	m.releaseLease = release
	return nil
}

func (m *Manager) releaseLeaseLocked() {
	if m.releaseLease != nil {
		m.releaseLease()
		m.releaseLease = nil
	}
}

func (m *Manager) IsEnabled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.enabled
}

// NeedsCleanup also tracks partial failures that never successfully enabled a proxy.
func (m *Manager) NeedsCleanup() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.restore != nil || m.clearing
}

func (m *Manager) snapshot() (func() error, error) {
	switch m.platform {
	case "darwin":
		return m.snapshotMacOS()
	case "windows":
		return m.snapshotWindows()
	case "linux":
		return m.snapshotLinux()
	default:
		return nil, fmt.Errorf("不支持的操作系统: %s", m.platform)
	}
}
