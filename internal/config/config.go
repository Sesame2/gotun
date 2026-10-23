package config

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

// SubnetAlias 定义网段映射规则
type SubnetAlias struct {
	Src *net.IPNet
	Dst *net.IPNet
}

// JumpHost 定义跳板机配置
type JumpHost struct {
	Host     string `json:"host"`
	Port     string `json:"port"`
	User     string `json:"user"`
	Password string `json:"password,omitempty"`
}

// Config 存储应用配置
type Config struct {
	ListenAddr      string
	SSHServer       string
	SSHUser         string
	SSHPassword     string
	SSHKeyFile      string
	KnownHostsFile  string // Empty uses ~/.ssh/known_hosts.
	HostKeyPrompt   func(host, fingerprint string) bool
	Context         context.Context
	HTTPUpstream    string        // 强制 HTTP 上游 (原 SSHTargetDial)
	SSHPort         string        // 添加SSH端口配置
	SocksAddr       string        // SOCKS5 监听地址
	TunMode         bool          // 是否启用 TUN 模式
	TunCIDR         string        // TUN 设备 CIDR (e.g. 10.0.0.1/24)
	TunRoute        []string      // 需要路由到 TUN 的网段
	TunGlobal       bool          // 是否开启全局模式
	SubnetAliases   []SubnetAlias // 网段/IP映射规则 (NAT)
	JumpHosts       []string      // 跳板机列表 (CLI/Legacy)
	JumpHostsList   []JumpHost    // 跳板机列表 (GUI/Structured)
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
		ListenAddr:      "127.0.0.1:8080",
		SSHServer:       "",
		SSHPort:         "22", // 默认SSH端口
		JumpHosts:       []string{},
		JumpHostsList:   []JumpHost{},
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
	if err := validatePort(port); err != nil {
		return "", "", "", err
	}

	return user, host, port, nil
}

func validatePort(port string) error {
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("无效的SSH端口: %s (应为 1-65535)", port)
	}
	return nil
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

	// 验证跳板机格式 (CLI/Legacy)
	for _, jumpHost := range c.JumpHosts {
		if jumpHost == "" {
			continue
		}
		_, _, _, err := parseJumpHost(jumpHost)
		if err != nil {
			return fmt.Errorf("跳板机格式错误: %v", err)
		}
	}

	// 验证结构化跳板机配置 (GUI)
	for i, jh := range c.JumpHostsList {
		if jh.Host == "" {
			return fmt.Errorf("第 %d 个跳板机配置缺少主机地址", i+1)
		}
		if jh.User == "" {
			return fmt.Errorf("第 %d 个跳板机配置缺少用户名", i+1)
		}
		if jh.Port == "" {
			c.JumpHostsList[i].Port = "22"
		}
		if err := validatePort(c.JumpHostsList[i].Port); err != nil {
			return fmt.Errorf("第 %d 个跳板机配置: %w", i+1, err)
		}
	}

	return nil
}

// GetJumpHostInfo 获取跳板机信息
func (c *Config) GetJumpHostInfo(jumpHost string) (user, host, port string, err error) {
	return parseJumpHost(jumpHost)
}

// ParseSubnetAlias 解析子网别名规则 (格式: SrcCIDR:DstCIDR)
func ParseSubnetAlias(alias string) (SubnetAlias, error) {
	parts := strings.Split(alias, ":")
	if len(parts) != 2 {
		return SubnetAlias{}, fmt.Errorf("无效的NAT映射格式: %s (应为 SrcCIDR:DstCIDR)", alias)
	}

	_, srcNet, err := net.ParseCIDR(parts[0])
	if err != nil {
		return SubnetAlias{}, fmt.Errorf("无效的源CIDR: %s (%v)", parts[0], err)
	}

	_, dstNet, err := net.ParseCIDR(parts[1])
	if err != nil {
		return SubnetAlias{}, fmt.Errorf("无效的目标CIDR: %s (%v)", parts[1], err)
	}
	srcPrefix, _ := srcNet.Mask.Size()
	dstPrefix, _ := dstNet.Mask.Size()
	if srcPrefix < dstPrefix {
		return SubnetAlias{}, fmt.Errorf("目标网段必须容纳源网段的所有地址: %s -> %s", srcNet, dstNet)
	}

	return SubnetAlias{
		Src: srcNet,
		Dst: dstNet,
	}, nil
}
