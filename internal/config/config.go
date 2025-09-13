package config

import (
	"errors"
	"flag"
	"time"
)

// Config 存储应用配置
type Config struct {
	ListenAddr    string
	SSHServer     string
	SSHUser       string
	SSHPassword   string
	SSHKeyFile    string
	SSHTargetDial string
	Timeout       time.Duration
	Verbose       bool
	LogFile       string
}

// NewConfig 创建默认配置
func NewConfig() *Config {
	return &Config{
		ListenAddr: ":8080",
		SSHServer:  "",
		Timeout:    10 * time.Second,
		Verbose:    false,
	}
}

// ParseFlags 解析命令行参数
func (c *Config) ParseFlags() {
	flag.StringVar(&c.ListenAddr, "listen", c.ListenAddr, "本地HTTP代理监听地址")
	flag.StringVar(&c.SSHServer, "ssh-server", c.SSHServer, "SSH服务器地址")
	flag.StringVar(&c.SSHUser, "ssh-user", c.SSHUser, "SSH用户名")
	flag.StringVar(&c.SSHPassword, "ssh-password", c.SSHPassword, "SSH密码")
	flag.StringVar(&c.SSHKeyFile, "ssh-key", c.SSHKeyFile, "SSH私钥文件路径")
	flag.StringVar(&c.SSHTargetDial, "target-network", c.SSHTargetDial, "可选的目标网络覆盖")
	flag.DurationVar(&c.Timeout, "timeout", c.Timeout, "连接超时时间")
	flag.BoolVar(&c.Verbose, "verbose", c.Verbose, "启用详细日志")
	flag.StringVar(&c.LogFile, "log-file", c.LogFile, "日志文件路径 (默认输出到标准输出)")

	flag.Parse()
}

// Validate 验证配置
func (c *Config) Validate() error {
	if c.SSHServer == "" {
		return errors.New("必须提供SSH服务器地址")
	}

	if c.SSHUser == "" {
		return errors.New("必须提供SSH用户名")
	}

	if c.SSHPassword == "" && c.SSHKeyFile == "" {
		return errors.New("必须提供SSH密码或私钥文件")
	}

	return nil
}
