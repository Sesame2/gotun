package service

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"sync"
	"time"

	"github.com/Sesame2/gotun/internal/config"
	"github.com/Sesame2/gotun/internal/logger"
	"github.com/Sesame2/gotun/internal/proxy"
	"github.com/Sesame2/gotun/internal/tun"
)

// TunStatus TUN服务状态
type TunStatus string

const (
	TunStatusStopped  TunStatus = "stopped"
	TunStatusStarting TunStatus = "starting"
	TunStatusRunning  TunStatus = "running"
	TunStatusStopping TunStatus = "stopping"
	TunStatusError    TunStatus = "error"
)

// TunConfig TUN配置 (GUI专用)
type TunConfig struct {
	TunMode   bool         `json:"tunMode"`
	TunGlobal bool         `json:"tunGlobal"`
	TunCIDR   string       `json:"tunCIDR"`
	TunRoutes []string     `json:"tunRoutes"`
	TunNAT    []TunNATRule `json:"tunNAT"`
}

// Validate 验证TUN配置
func (c *TunConfig) Validate() error {
	if !c.TunMode {
		return nil // TUN模式未启用，无需验证
	}

	// 验证 CIDR
	if c.TunCIDR != "" {
		ip, network, err := net.ParseCIDR(c.TunCIDR)
		if err != nil {
			return fmt.Errorf("无效的TUN CIDR: %s (%v)", c.TunCIDR, err)
		}
		if _, bits := network.Mask.Size(); ip.To4() == nil || bits != 32 {
			return fmt.Errorf("TUN CIDR 仅支持 IPv4: %s", c.TunCIDR)
		}
	}

	// 验证路由
	for _, route := range c.TunRoutes {
		ip, network, err := net.ParseCIDR(route)
		if err != nil {
			return fmt.Errorf("无效的路由CIDR: %s (%v)", route, err)
		}
		if _, bits := network.Mask.Size(); ip.To4() == nil || bits != 32 {
			return fmt.Errorf("TUN 路由仅支持 IPv4: %s", route)
		}
	}

	// 验证 NAT 规则
	for _, nat := range c.TunNAT {
		_, err := config.ParseSubnetAlias(nat.SrcCIDR + ":" + nat.DstCIDR)
		if err != nil {
			return fmt.Errorf("无效的NAT映射: %w", err)
		}
	}

	return nil
}

// TunStats TUN服务统计信息
type TunStats struct {
	ProfileID    string    `json:"profileId,omitempty"`
	Status       TunStatus `json:"status"`
	TunIP        string    `json:"tunIP,omitempty"`
	TunDevice    string    `json:"tunDevice,omitempty"`
	IsGlobal     bool      `json:"isGlobal"`
	Routes       []string  `json:"routes,omitempty"`
	NATRules     []string  `json:"natRules,omitempty"`
	StartTime    time.Time `json:"startTime,omitempty"`
	Uptime       string    `json:"uptime,omitempty"`
	ErrorMessage string    `json:"errorMessage,omitempty"`
}

type nativeTun interface {
	Start() error
	Close() error
}

// TunService TUN服务管理
type TunService struct {
	lifecycleMu    sync.Mutex
	newNative      func(*config.Config, *logger.Logger, *proxy.SSHClient) (nativeTun, error)
	needsElevation func() bool
	mu             sync.RWMutex
	cfg            *TunConfig
	fullCfg        *config.Config // 完整配置(用于SSH连接)
	logger         *logger.Logger
	sshClient      *proxy.SSHClient
	tunService     nativeTun
	status         TunStatus
	startTime      time.Time
	errMsg         string
}

// NewTunService 创建TUN服务
func NewTunService() *TunService {
	return &TunService{
		newNative: func(cfg *config.Config, log *logger.Logger, ssh *proxy.SSHClient) (nativeTun, error) {
			return tun.NewTunService(cfg, log, ssh)
		},
		cfg: &TunConfig{
			TunCIDR: "10.0.0.1/24", // 默认CIDR
		},
		logger: logger.NewLogger(false),
		status: TunStatusStopped,
	}
}

// SetLogCallback 设置日志回调
func (s *TunService) SetLogCallback(cb logger.LogCallback) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.logger != nil {
		s.logger.SetCallback(cb)
	}
}

// log 记录日志
func (s *TunService) log(level, message string) {
	if s.logger != nil {
		switch level {
		case "INFO":
			s.logger.Info(message)
		case "WARN":
			s.logger.Warn(message)
		case "ERROR":
			s.logger.Error(message)
		case "DEBUG":
			s.logger.Debug(message)
		}
	}
}

func (s *TunService) SetVerbose(verbose bool) { s.logger.SetVerbose(verbose) }

func (s *TunService) SetLogFile(path string) error { return s.logger.SetLogFile(path) }

// SetConfig 设置TUN配置
func (s *TunService) SetConfig(cfg *TunConfig) {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status == TunStatusRunning || s.status == TunStatusStarting || s.status == TunStatusStopping {
		return
	}
	if cfg == nil {
		s.cfg = nil
		return
	}
	snapshot := *cfg
	snapshot.TunRoutes = slices.Clone(cfg.TunRoutes)
	snapshot.TunNAT = slices.Clone(cfg.TunNAT)
	s.cfg = &snapshot
}

// SetFullConfig 设置完整配置(包含SSH信息)
func (s *TunService) SetFullConfig(cfg *config.Config) {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status == TunStatusRunning || s.status == TunStatusStarting || s.status == TunStatusStopping {
		return
	}
	if cfg == nil {
		s.fullCfg = nil
		return
	}
	snapshot := *cfg
	snapshot.JumpHosts = slices.Clone(cfg.JumpHosts)
	snapshot.JumpHostsList = slices.Clone(cfg.JumpHostsList)
	snapshot.TunRoute = slices.Clone(cfg.TunRoute)
	s.fullCfg = &snapshot
}

// GetStatus 获取TUN服务状态
func (s *TunService) GetStatus() TunStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := TunStats{
		Status:       s.status,
		IsGlobal:     s.cfg != nil && s.cfg.TunGlobal,
		ErrorMessage: s.errMsg,
	}

	if s.cfg != nil {
		stats.Routes = slices.Clone(s.cfg.TunRoutes)
		// 转换 NAT 规则为字符串列表
		for _, nat := range s.cfg.TunNAT {
			stats.NATRules = append(stats.NATRules, fmt.Sprintf("%s -> %s", nat.SrcCIDR, nat.DstCIDR))
		}
	}

	if s.status == TunStatusRunning && !s.startTime.IsZero() {
		stats.StartTime = s.startTime
		stats.Uptime = time.Since(s.startTime).Round(time.Second).String()
	}

	return stats
}

// RequiresElevation 检查是否需要提权
func (s *TunService) RequiresElevation() bool {
	switch runtime.GOOS {
	case "darwin", "linux":
		return os.Geteuid() != 0
	case "windows":
		// Windows 需要检查是否以管理员身份运行
		return !isWindowsAdmin()
	default:
		return false
	}
}

// isWindowsAdmin 检查Windows是否以管理员身份运行
func isWindowsAdmin() bool {
	if runtime.GOOS != "windows" {
		return false
	}

	// 尝试打开一个需要管理员权限的操作
	// 这里使用一个简单的检测方法
	file, err := os.Open("\\\\.\\PHYSICALDRIVE0")
	if err != nil {
		return false
	}
	_ = file.Close()
	return true
}

// GetElevationCommand 获取提权命令
func (s *TunService) GetElevationCommand() []string {
	execPath, err := os.Executable()
	if err != nil {
		return nil
	}

	switch runtime.GOOS {
	case "darwin":
		// macOS 使用 osascript 进行提权
		// 使用 nohup 和 & 让命令在后台运行
		script := fmt.Sprintf(`do shell script "nohup '%s' > /dev/null 2>&1 &" with administrator privileges`, execPath)
		return []string{"osascript", "-e", script}
	case "linux":
		// Linux 优先使用 pkexec
		if _, err := exec.LookPath("pkexec"); err == nil {
			return []string{"pkexec", execPath}
		}
		// 回退到 sudo (需要终端)
		return []string{"sudo", execPath}
	case "windows":
		// Windows 使用 runas 或 PowerShell
		return []string{"powershell", "-Command", fmt.Sprintf("Start-Process '%s' -Verb RunAs", execPath)}
	default:
		return nil
	}
}

// Start 启动TUN服务
func (s *TunService) Start() error {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	s.mu.Lock()
	if s.status == TunStatusRunning || s.status == TunStatusStarting {
		s.mu.Unlock()
		return fmt.Errorf("TUN服务已在运行中")
	}

	if s.tunService != nil || s.sshClient != nil {
		s.mu.Unlock()
		return fmt.Errorf("请先停止并清理上次的TUN服务")
	}
	if s.cfg == nil || !s.cfg.TunMode {
		s.mu.Unlock()
		return fmt.Errorf("TUN模式未启用")
	}

	if s.fullCfg == nil {
		s.mu.Unlock()
		return fmt.Errorf("未设置完整配置")
	}

	s.status = TunStatusStarting
	s.errMsg = ""
	s.mu.Unlock()

	// 验证配置
	if err := s.cfg.Validate(); err != nil {
		s.setError(fmt.Sprintf("配置错误: %v", err))
		return err
	}

	// 检查权限
	needsElevation := s.RequiresElevation
	if s.needsElevation != nil {
		needsElevation = s.needsElevation
	}
	if needsElevation() {
		err := fmt.Errorf("TUN模式需要管理员权限")
		s.setError(err.Error())
		return err
	}

	s.log("INFO", "正在启动TUN服务...")

	// 将 GUI 配置合并到完整配置
	s.fullCfg.TunMode = s.cfg.TunMode
	s.fullCfg.TunGlobal = s.cfg.TunGlobal
	if s.cfg.TunCIDR != "" {
		s.fullCfg.TunCIDR = s.cfg.TunCIDR
	}
	s.fullCfg.TunRoute = s.cfg.TunRoutes

	// 转换 NAT 规则
	s.fullCfg.SubnetAliases = nil
	for _, nat := range s.cfg.TunNAT {
		alias, err := config.ParseSubnetAlias(nat.SrcCIDR + ":" + nat.DstCIDR)
		if err != nil {
			s.setError(err.Error())
			return err
		}
		s.fullCfg.SubnetAliases = append(s.fullCfg.SubnetAliases, alias)
	}
	if err := s.fullCfg.Validate(); err != nil {
		s.setError(err.Error())
		return err
	}

	// 修正: GUI环境下禁用交互式认证
	if s.fullCfg.SSHPassword != "" {
		s.fullCfg.InteractiveAuth = false
	}

	// 初始化 SSH 连接
	sshClient, err := proxy.NewSSHClient(s.fullCfg, s.logger)
	if err != nil {
		errMsg := fmt.Sprintf("SSH连接失败: %v", err)
		s.setError(errMsg)
		return fmt.Errorf("SSH连接失败: %w", err)
	}
	s.sshClient = sshClient
	s.log("INFO", "SSH连接已建立")

	// 初始化 TUN 服务
	tunService, err := s.newNative(s.fullCfg, s.logger, sshClient)
	if err != nil {
		sshClient.Close()
		s.sshClient = nil
		errMsg := fmt.Sprintf("TUN服务初始化失败: %v", err)
		s.setError(errMsg)
		return fmt.Errorf("TUN服务初始化失败: %w", err)
	}
	s.tunService = tunService

	// Native Start completes setup and starts its own pumps before returning.
	if err := tunService.Start(); err != nil {
		startErr := fmt.Errorf("TUN服务启动失败: %w", err)
		cleanupErr := s.closeResources()
		err = errors.Join(startErr, cleanupErr)
		s.setError(err.Error())
		return err
	}

	s.mu.Lock()
	s.status = TunStatusRunning
	s.startTime = time.Now()
	s.mu.Unlock()

	mode := "路由模式"
	if s.cfg.TunGlobal {
		mode = "全局模式"
	}
	s.log("INFO", fmt.Sprintf("TUN服务已启动 (%s)", mode))

	return nil
}

// Stop 停止TUN服务
func (s *TunService) Stop() error {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	s.mu.Lock()
	if s.status == TunStatusStopped && s.tunService == nil && s.sshClient == nil {
		s.mu.Unlock()
		return nil
	}
	s.status = TunStatusStopping
	s.mu.Unlock()
	s.log("INFO", "正在停止TUN服务...")
	err := s.closeResources()
	s.mu.Lock()
	s.startTime = time.Time{}
	if err != nil {
		s.status = TunStatusError
		s.errMsg = err.Error()
	} else {
		s.status = TunStatusStopped
		s.errMsg = ""
	}
	s.mu.Unlock()
	if err != nil {
		return err
	}
	s.log("INFO", "TUN服务已停止")
	return nil
}

// A failed native close retains the handle so Stop can retry cleanup.
func (s *TunService) closeResources() error {
	var failures []error
	if s.tunService != nil {
		if err := s.tunService.Close(); err != nil {
			failures = append(failures, fmt.Errorf("关闭TUN服务失败: %w", err))
		} else {
			s.tunService = nil
		}
	}
	if s.sshClient != nil {
		if err := s.sshClient.Close(); err != nil {
			failures = append(failures, fmt.Errorf("关闭SSH连接失败: %w", err))
		} else {
			s.sshClient = nil
		}
	}
	return errors.Join(failures...)
}

// setStatus 设置状态
func (s *TunService) setStatus(status TunStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = status
}

// setError 设置错误状态
func (s *TunService) setError(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = TunStatusError
	s.errMsg = msg
}

// IsRunning 检查服务是否运行中
func (s *TunService) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status == TunStatusRunning
}
