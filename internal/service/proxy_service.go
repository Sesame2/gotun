package service

import (
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

// ProxyService 代理服务管理
type ProxyService struct {
	mu         sync.RWMutex
	cfg        *config.Config
	logger     *logger.Logger
	sshClient  *proxy.SSHClient
	httpProxy  *proxy.HTTPOverSSH
	socksProxy *proxy.SOCKS5OverSSH
	proxyMgr   *sysproxy.Manager
	router     *router.Router
	status     ProxyStatus
	startTime  time.Time
	errMsg     string
	requests   int64
}

// NewProxyService 创建代理服务
func NewProxyService() *ProxyService {
	return &ProxyService{
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
	s.mu.Lock()
	defer s.mu.Unlock()
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
		SystemProxy:   s.cfg.SystemProxy,
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
	s.mu.Lock()
	if s.status == StatusRunning || s.status == StatusStarting {
		s.mu.Unlock()
		return fmt.Errorf("代理服务已在运行中")
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
	if s.cfg.SystemProxy {
		s.proxyMgr = sysproxy.NewManager(s.logger, s.cfg.ListenAddr, s.cfg.SocksAddr)
	}

	// 启动 HTTP 代理
	go func() {
		if s.cfg.SystemProxy && s.proxyMgr != nil {
			if err := s.proxyMgr.Enable(); err != nil {
				s.logger.Errorf("设置系统代理失败: %v", err)
			}
		}
		if err := s.httpProxy.Start(); err != nil {
			s.logger.Errorf("HTTP代理服务启动失败: %v", err)
			s.setError(fmt.Sprintf("HTTP代理服务启动失败: %v", err))
		}
	}()

	// 启动 SOCKS5 代理
	if s.socksProxy != nil {
		go func() {
			if err := s.socksProxy.Start(); err != nil {
				s.logger.Errorf("SOCKS5代理服务启动失败: %v", err)
			}
		}()
	}

	s.mu.Lock()
	s.status = StatusRunning
	s.startTime = time.Now()
	s.mu.Unlock()

	s.logger.Infof("代理服务已启动 - HTTP: %s", s.cfg.ListenAddr)
	if s.cfg.SocksAddr != "" {
		s.logger.Infof("SOCKS5: %s", s.cfg.SocksAddr)
	}

	return nil
}

// Stop 停止代理服务
func (s *ProxyService) Stop() error {
	s.mu.Lock()
	if s.status != StatusRunning {
		s.mu.Unlock()
		return nil
	}
	s.status = StatusStopping
	s.mu.Unlock()

	s.logger.Info("正在停止代理服务...")

	// 恢复系统代理
	if s.cfg.SystemProxy && s.proxyMgr != nil {
		if err := s.proxyMgr.Disable(); err != nil {
			s.logger.Errorf("恢复系统代理设置失败: %v", err)
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
	return nil
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
