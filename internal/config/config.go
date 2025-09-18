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
	SSHPort         string // 添加SSH端口配置
	Timeout         time.Duration
	Verbose         bool
	LogFile         string
	InteractiveAuth bool
	SystemProxy     bool   // 是否启用系统代理
	SystemProxyPac  string // PAC文件URL，如果需要
}

// NewConfig 创建默认配置
func NewConfig() *Config {
	return &Config{
		ListenAddr:      ":8080",
		SSHServer:       "",
		SSHPort:         "22", // 默认SSH端口
		Timeout:         10 * time.Second,
		Verbose:         false,
		InteractiveAuth: true,
		SystemProxy:     true,
		SystemProxyPac:  "",
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

// ParseFlags 解析命令行参数
func (c *Config) ParseFlags() {
	// 定义SSH端口参数
	flag.StringVar(&c.SSHPort, "p", c.SSHPort, "SSH服务器端口")

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

	// 解析标准参数
	flag.Parse()

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

	return nil
}
