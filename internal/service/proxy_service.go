package service

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Sesame2/gotun/internal/config"
	"github.com/Sesame2/gotun/internal/logger"
	"github.com/Sesame2/gotun/internal/proxy"
	"github.com/Sesame2/gotun/internal/router"
	"github.com/Sesame2/gotun/internal/sysproxy"
)

// ProxyStatus 代理服务状态
type ProxyStatus string

const (
	StatusStopped  ProxyStatus = "stopped"
	StatusStarting ProxyStatus = "starting"
	StatusRunning  ProxyStatus = "running"
	StatusStopping ProxyStatus = "stopping"
	StatusError    ProxyStatus = "error"
)

// ProxyStats 代理统计信息
type ProxyStats struct {
	ProfileID     string      `json:"profileId,omitempty"`
	Status        ProxyStatus `json:"status"`
	HTTPAddr      string      `json:"httpAddr"`
	SocksAddr     string      `json:"socksAddr,omitempty"`
	SSHServer     string      `json:"sshServer"`
	SSHUser       string      `json:"sshUser"`
	JumpHosts     []string    `json:"jumpHosts,omitempty"`
	SystemProxy   bool        `json:"systemProxy"`
	StartTime     time.Time   `json:"startTime,omitempty"`
	Uptime        string      `json:"uptime,omitempty"`
	ErrorMessage  string      `json:"errorMessage,omitempty"`
	TotalRequests int64       `json:"totalRequests"`
}

type systemProxyManager interface {
	Enable() error
	Disable() error
	Clear() error
	IsEnabled() bool
}

// ProxyService 代理服务管理
type ProxyService struct {
	newSystemProxy func(*logger.Logger, string, string) systemProxyManager
	lifecycleMu    sync.Mutex
	mu             sync.RWMutex
	cfg            *config.Config
	logger         *logger.Logger
	sshClient      *proxy.SSHClient
	httpProxy      *proxy.HTTPOverSSH
	socksProxy     *proxy.SOCKS5OverSSH
	proxyMgr       systemProxyManager
	router         *router.Router
	status         ProxyStatus
	startTime      time.Time
	errMsg         string
	requests       int64
}

// NewProxyService 创建代理服务
func NewProxyService() *ProxyService {
	return &ProxyService{
		newSystemProxy: func(log *logger.Logger, httpAddr, socksAddr string) systemProxyManager {
			return sysproxy.NewManager(log, httpAddr, socksAddr)
		},
		cfg:    config.NewConfig(),
		logger: logger.NewLogger(false),
		status: StatusStopped,
	}
}

// SetLogCallback 设置日志回调函数
func (s *ProxyService) SetLogCallback(cb logger.LogCallback) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logger.SetCallback(cb)
}

// SetConfig 设置配置
func (s *ProxyService) SetConfig(cfg *config.Config) {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if cfg == nil || s.status == StatusRunning || s.status == StatusStarting || s.status == StatusStopping {
		return
	}
	s.cfg = cfg
}

// GetConfig 获取当前配置
func (s *ProxyService) GetConfig() *config.Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

// SetVerbose 设置详细日志模式
func (s *ProxyService) SetVerbose(verbose bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// 保存当前的 callback
	oldCallback := s.logger.GetCallback()
	s.logger = logger.NewLogger(verbose)
	// 恢复 callback
	if oldCallback != nil {
		s.logger.SetCallback(oldCallback)
	}
}

// SetLogFile 设置日志文件
func (s *ProxyService) SetLogFile(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.logger.SetLogFile(path)
}

// GetStatus 获取代理服务状态
func (s *ProxyService) GetStatus() ProxyStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := ProxyStats{
		Status:        s.status,
		HTTPAddr:      s.cfg.ListenAddr,
		SocksAddr:     s.cfg.SocksAddr,
		SSHServer:     s.cfg.SSHServer,
		SSHUser:       s.cfg.SSHUser,
		JumpHosts:     s.cfg.JumpHosts,
		SystemProxy:   s.proxyMgr != nil && s.proxyMgr.IsEnabled(),
		ErrorMessage:  s.errMsg,
		TotalRequests: s.requests,
	}

	if s.status == StatusRunning && !s.startTime.IsZero() {
		stats.StartTime = s.startTime
		stats.Uptime = time.Since(s.startTime).Round(time.Second).String()
	}

	return stats
}

// Start 启动代理服务
func (s *ProxyService) Start() error {
	return s.start(nil)
}

// StartWithConfig selects and starts a profile within the same lifecycle lock.
func (s *ProxyService) StartWithConfig(cfg *config.Config) error {
	if cfg == nil {
		return fmt.Errorf("代理配置不能为空")
	}
	return s.start(cfg)
}

func (s *ProxyService) start(cfg *config.Config) error {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	s.mu.Lock()
	if s.status == StatusRunning || s.status == StatusStarting {
		s.mu.Unlock()
		return fmt.Errorf("代理服务已在运行中")
	}
	pending := s.proxyMgr
	s.mu.Unlock()
	// Preserve tracking until cleanup of the previous OS integration succeeds.
	if pending != nil && pending.IsEnabled() {
		if err := pending.Disable(); err != nil {
			failure := fmt.Errorf("清理上次系统代理失败: %w", err)
			s.setError(failure.Error())
			return failure
		}
	}
	s.mu.Lock()
	if cfg != nil {
		s.cfg = cfg
	}
	s.status = StatusStarting
	s.errMsg = ""
	s.mu.Unlock()

	// 验证配置
	if err := s.cfg.Validate(); err != nil {
		s.setError(fmt.Sprintf("配置错误: %v", err))
		return err
	}

	// 修正: 如果提供了密码，则禁用交互式认证，防止在无终端环境下尝试读取stdin导致失败
	if s.cfg.SSHPassword != "" {
		s.logger.Debug("检测到已配置密码，自动禁用交互式认证")
		s.cfg.InteractiveAuth = false
	} else if len(s.cfg.SSHKeyFile) == 0 && s.cfg.InteractiveAuth {
		// 即使没提供密码，如果是GUI环境且没有终端，交互式认证也会失败。
		// 但为了保险起见，我们只在有明确替代方案(密码)时禁用它，
		// 或者留给底层去尝试(虽然会失败)。
		// 更好的做法可能是: 对于GUI Service，默认就是非交互式的。
		// 这里暂且只处理"有密码但因为Interactive=true而失败"的情况。
	}

	s.logger.Infof("GoTun GUI 代理服务启动中...")

	// 1. 初始化 Router
	s.router = nil
	if s.cfg.RuleFile != "" {
		r, err := router.NewRouter(s.cfg.RuleFile)
		if err != nil {
			s.logger.Warnf("加载规则文件失败: %v。将以全局代理模式运行。", err)
		} else {
			s.router = r
			s.logger.Infof("已加载规则文件: %s", s.cfg.RuleFile)
		}
	}

	// 2. 初始化 SSHClient
	sshClient, err := proxy.NewSSHClient(s.cfg, s.logger)
	if err != nil {
		errMsg := fmt.Sprintf("SSH连接失败: %v", err)
		s.setError(errMsg)
		return fmt.Errorf("%s", errMsg)
	}
	s.sshClient = sshClient

	// 3. 初始化 HTTP 代理
	httpProxy, err := proxy.NewHTTPOverSSH(s.cfg, s.logger, sshClient, s.router)
	if err != nil {
		sshClient.Close()
		errMsg := fmt.Sprintf("HTTP代理初始化失败: %v", err)
		s.setError(errMsg)
		return fmt.Errorf("%s", errMsg)
	}
	s.httpProxy = httpProxy

	// 4. 初始化 SOCKS5 代理（可选）
	if s.cfg.SocksAddr != "" {
		socksProxy, err := proxy.NewSOCKS5OverSSH(s.cfg, s.logger, sshClient, s.router)
		if err != nil {
			httpProxy.Close()
			sshClient.Close()
			errMsg := fmt.Sprintf("SOCKS5代理初始化失败: %v", err)
			s.setError(errMsg)
			return fmt.Errorf("%s", errMsg)
		}
		s.socksProxy = socksProxy
	}

	// 5. 系统代理管理器
	s.mu.Lock()
	s.proxyMgr = s.newSystemProxy(s.logger, s.cfg.ListenAddr, s.cfg.SocksAddr)
	mgr := s.proxyMgr
	s.mu.Unlock()
	if s.cfg.SystemProxy {
		if err := mgr.Enable(); err != nil {
			failure := fmt.Errorf("设置系统代理失败: %w", err)
			if mgr.IsEnabled() {
				failure = errors.Join(failure, mgr.Disable())
			}
			if s.httpProxy != nil {
				failure = errors.Join(failure, s.httpProxy.Close())
				s.httpProxy = nil
			}
			if s.socksProxy != nil {
				failure = errors.Join(failure, s.socksProxy.Close())
				s.socksProxy = nil
			}
			if s.sshClient != nil {
				failure = errors.Join(failure, s.sshClient.Close())
				s.sshClient = nil
			}
			s.setError(failure.Error())
			return failure
		}
	}

	s.mu.Lock()
	s.status = StatusRunning
	s.startTime = time.Now()
	s.mu.Unlock()

	// 启动 HTTP 代理
	log := s.logger
	go func() {
		if err := httpProxy.Start(); err != nil {
			log.Errorf("HTTP代理服务启动失败: %v", err)
			s.setError(fmt.Sprintf("HTTP代理服务启动失败: %v", err))
		}
	}()

	// 启动 SOCKS5 代理
	if socksProxy := s.socksProxy; socksProxy != nil {
		go func() {
			if err := socksProxy.Start(); err != nil {
				log.Errorf("SOCKS5代理服务启动失败: %v", err)
				s.setError(fmt.Sprintf("SOCKS5代理服务启动失败: %v", err))
			}
		}()
	}

	s.logger.Infof("代理服务已启动 - HTTP: %s", s.cfg.ListenAddr)
	if s.cfg.SocksAddr != "" {
		s.logger.Infof("SOCKS5: %s", s.cfg.SocksAddr)
	}

	return nil
}

// Stop 停止代理服务
func (s *ProxyService) Stop() error {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	s.mu.Lock()
	if s.status == StatusStopped && (s.proxyMgr == nil || !s.proxyMgr.IsEnabled()) {
		s.mu.Unlock()
		return nil
	}
	s.status = StatusStopping
	s.mu.Unlock()

	s.logger.Info("正在停止代理服务...")

	// 恢复系统代理
	var proxyErr error
	if s.proxyMgr != nil {
		if err := s.proxyMgr.Disable(); err != nil {
			s.logger.Errorf("恢复系统代理设置失败: %v", err)
			proxyErr = err
		}
	}

	// 关闭 HTTP 代理
	if s.httpProxy != nil {
		if err := s.httpProxy.Close(); err != nil {
			s.logger.Errorf("关闭HTTP代理服务失败: %v", err)
		}
		s.httpProxy = nil
	}

	// 关闭 SOCKS5 代理
	if s.socksProxy != nil {
		if err := s.socksProxy.Close(); err != nil {
			s.logger.Errorf("关闭SOCKS5代理服务失败: %v", err)
		}
		s.socksProxy = nil
	}

	// 关闭 SSH 连接
	if s.sshClient != nil {
		s.sshClient.Close()
		s.sshClient = nil
	}

	s.mu.Lock()
	s.status = StatusStopped
	s.startTime = time.Time{}
	s.mu.Unlock()

	s.logger.Info("代理服务已停止")
	return proxyErr
}

// Restart 重启代理服务
func (s *ProxyService) Restart() error {
	if err := s.Stop(); err != nil {
		return err
	}
	time.Sleep(500 * time.Millisecond) // 等待资源释放
	return s.Start()
}

// TestConnection 测试SSH连接
func (s *ProxyService) TestConnection(cfg *config.Config) error {
	testLogger := logger.NewLogger(true)

	// 修正: GUI环境下测试连接时，如果有密码，应禁用交互式认证
	if cfg.SSHPassword != "" {
		cfg.InteractiveAuth = false
	}

	// 尝试建立SSH连接
	client, err := proxy.NewSSHClient(cfg, testLogger)
	if err != nil {
		return fmt.Errorf("SSH连接测试失败: %v", err)
	}

	// 关闭测试连接
	client.Close()
	return nil
}

// setError 设置错误状态
func (s *ProxyService) setError(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = StatusError
	s.errMsg = msg
}

// IsRunning 检查服务是否运行中
func (s *ProxyService) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status == StatusRunning
}

// SetSystemProxy changes the running HTTP proxy's integration without restarting SSH.
func (s *ProxyService) SetSystemProxy(enabled bool) error {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	s.mu.Lock()
	if enabled && s.status != StatusRunning {
		s.mu.Unlock()
		return fmt.Errorf("代理服务尚未运行")
	}
	if s.proxyMgr == nil {
		if !enabled {
			s.mu.Unlock()
			return nil
		}
		s.proxyMgr = s.newSystemProxy(s.logger, s.cfg.ListenAddr, s.cfg.SocksAddr)
	}
	mgr := s.proxyMgr
	s.mu.Unlock()
	if enabled {
		return mgr.Enable()
	}
	return mgr.Disable()
}

// ClearSystemProxy clears lingering OS proxy settings independently of the tunnel lifecycle.
func (s *ProxyService) ClearSystemProxy() error {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	s.mu.Lock()
	if s.proxyMgr == nil {
		s.proxyMgr = s.newSystemProxy(s.logger, s.cfg.ListenAddr, s.cfg.SocksAddr)
	}
	mgr := s.proxyMgr
	s.mu.Unlock()
	return mgr.Clear()
}
