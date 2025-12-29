package config

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

// SubnetAlias 定义网段映射规则
type SubnetAlias struct {
	Src *net.IPNet
	Dst *net.IPNet
}

// Config 存储应用配置
type Config struct {
	ListenAddr      string
	SSHServer       string
	SSHUser         string
	SSHPassword     string
	SSHKeyFile      string
	HTTPUpstream    string        // 强制 HTTP 上游 (原 SSHTargetDial)
	SSHPort         string        // 添加SSH端口配置
	SocksAddr       string        // SOCKS5 监听地址
	TunMode         bool          // 是否启用 TUN 模式
	TunCIDR         string        // TUN 设备 CIDR (e.g. 10.0.0.1/24)
	TunRoute        []string      // 需要路由到 TUN 的网段
	TunGlobal       bool          // 是否开启全局模式
	SubnetAliases   []SubnetAlias // 网段/IP映射规则 (NAT)
	JumpHosts       []string      // 跳板机列表
	Timeout         time.Duration
	Verbose         bool
	LogFile         string
	InteractiveAuth bool
	SystemProxy     bool // 是否启用系统代理
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
		RuleFile:        "",
		SocksAddr:       "",
		TunMode:         false,
		TunCIDR:         "10.0.0.1/24",
		TunRoute:        []string{},
		TunGlobal:       false,
		SubnetAliases:   []SubnetAlias{},
	}
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
