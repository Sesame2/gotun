package config

import (
	"errors"
	"flag"
	"fmt"
	"strings"
	"time"
)

// Config 存储应用配置
type Config struct {
	ListenAddr      string
	SSHServer       string
	SSHUser         string
	SSHPassword     string
	SSHKeyFile      string
	SSHTargetDial   string
	SSHPort         string   // 添加SSH端口配置
	JumpHosts       []string // 跳板机列表
	Timeout         time.Duration
	Verbose         bool
	LogFile         string
	InteractiveAuth bool
	SystemProxy     bool   // 是否启用系统代理
	SystemProxyPac  string // PAC文件URL，如果需要
	RuleFile        string
}

// NewConfig 创建默认配置
func NewConfig() *Config {
	return &Config{
		ListenAddr:      ":8080",
		SSHServer:       "",
		SSHPort:         "22", // 默认SSH端口
		JumpHosts:       []string{},
		Timeout:         10 * time.Second,
		Verbose:         false,
		InteractiveAuth: true,
		SystemProxy:     true,
		SystemProxyPac:  "",
		RuleFile:        "",
	}
}

// 解析SSH目标格式(user@host)
func parseSSHTarget(target string) (string, string, error) {
	if target == "" {
		return "", "", nil
	}

	parts := strings.Split(target, "@")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("无效的SSH目标格式，需要 user@host 格式")
	}

	user := parts[0]
	host := parts[1]

	// 检查是否有效
	if user == "" || host == "" {
		return "", "", fmt.Errorf("用户名或主机名不能为空")
	}

	return user, host, nil
}

// parseJumpHost 解析跳板机格式
func parseJumpHost(jumpHost string) (user, host, port string, err error) {
	// 支持格式: user@host:port, user@host, host:port, host
	parts := strings.Split(jumpHost, "@")

	var hostPart string
	if len(parts) == 2 {
		user = parts[0]
		hostPart = parts[1]
	} else if len(parts) == 1 {
		hostPart = parts[0]
	} else {
		return "", "", "", fmt.Errorf("无效的跳板机格式: %s", jumpHost)
	}

	// 解析主机和端口
	if strings.Contains(hostPart, ":") {
		hostPortParts := strings.Split(hostPart, ":")
		if len(hostPortParts) != 2 {
			return "", "", "", fmt.Errorf("无效的主机:端口格式: %s", hostPart)
		}
		host = hostPortParts[0]
		port = hostPortParts[1]
	} else {
		host = hostPart
		port = "22" // 默认SSH端口
	}

	if host == "" {
		return "", "", "", fmt.Errorf("主机名不能为空")
	}

	return user, host, port, nil
}

// ParseFlags 解析命令行参数
func (c *Config) ParseFlags() {
	var jumpHostsStr string

	// 定义SSH端口参数
	flag.StringVar(&c.SSHPort, "p", c.SSHPort, "SSH服务器端口")

	// 定义跳板机参数
	flag.StringVar(&jumpHostsStr, "J", "", "跳板机列表，用逗号分隔 (格式: user@host:port)")

	// 定义其他标准参数
	flag.StringVar(&c.ListenAddr, "listen", c.ListenAddr, "本地HTTP代理监听地址")
	flag.StringVar(&c.SSHPassword, "pass", c.SSHPassword, "SSH密码")
	flag.StringVar(&c.SSHKeyFile, "i", c.SSHKeyFile, "SSH私钥文件路径")
	flag.StringVar(&c.SSHTargetDial, "target", c.SSHTargetDial, "可选的目标网络覆盖")
	flag.DurationVar(&c.Timeout, "timeout", c.Timeout, "连接超时时间")
	flag.BoolVar(&c.Verbose, "v", c.Verbose, "启用详细日志")
	flag.StringVar(&c.LogFile, "log", c.LogFile, "日志文件路径 (默认输出到标准输出)")
	flag.BoolVar(&c.SystemProxy, "sys-proxy", c.SystemProxy, "自动设置系统代理")
	flag.StringVar(&c.SystemProxyPac, "proxy-pac", c.SystemProxyPac, "代理自动配置(PAC)文件URL")
	flag.StringVar(&c.RuleFile, "rules", c.RuleFile, "代理规则配置文件路径")

	// 解析标准参数
	flag.Parse()

	// 解析跳板机列表
	if jumpHostsStr != "" {
		c.JumpHosts = strings.Split(jumpHostsStr, ",")
		// 清理空白字符
		for i, host := range c.JumpHosts {
			c.JumpHosts[i] = strings.TrimSpace(host)
		}
	}

	// 处理非标志参数(即 user@host 形式)
	args := flag.Args()
	if len(args) > 0 {
		sshTarget := args[0]
		user, host, err := parseSSHTarget(sshTarget)
		if err == nil {
			c.SSHUser = user
			c.SSHServer = host
		}
	}

	// 组合服务器地址和端口
	if c.SSHServer != "" && !strings.Contains(c.SSHServer, ":") {
		c.SSHServer = fmt.Sprintf("%s:%s", c.SSHServer, c.SSHPort)
	}
}

// Validate 验证配置
func (c *Config) Validate() error {
	if c.SSHServer == "" {
		return errors.New("必须提供SSH服务器地址")
	}

	if c.SSHUser == "" {
		return errors.New("必须提供SSH用户名")
	}

	if !c.InteractiveAuth && c.SSHPassword == "" && c.SSHKeyFile == "" {
		return errors.New("必须提供SSH密码、私钥文件或使用交互式认证")
	}

	// 验证跳板机格式
	for _, jumpHost := range c.JumpHosts {
		if jumpHost == "" {
			continue
		}
		_, _, _, err := parseJumpHost(jumpHost)
		if err != nil {
			return fmt.Errorf("跳板机格式错误: %v", err)
		}
	}

	return nil
}

// GetJumpHostInfo 获取跳板机信息
func (c *Config) GetJumpHostInfo(jumpHost string) (user, host, port string, err error) {
	return parseJumpHost(jumpHost)
}
