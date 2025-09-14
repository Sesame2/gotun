package proxy

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"

	"github.com/Sesame2/gotun/internal/config"
	"github.com/Sesame2/gotun/internal/logger"
	"github.com/Sesame2/gotun/internal/utils"
)

// SSHClient 管理SSH连接
type SSHClient struct {
	client *ssh.Client
	config *ssh.ClientConfig
	logger *logger.Logger
}

// NewSSHClient 创建SSH客户端
func NewSSHClient(cfg *config.Config, log *logger.Logger) (*SSHClient, error) {
	log.Infof("配置SSH客户端连接到 %s", cfg.SSHServer)

	authMethods := []ssh.AuthMethod{}

	// 添加私钥认证
	if cfg.SSHKeyFile != "" {
		log.Debugf("尝试从 %s 加载SSH私钥", cfg.SSHKeyFile)

		// 展开波浪号路径
		keyPath := cfg.SSHKeyFile
		if strings.HasPrefix(keyPath, "~/") {
			home, err := os.UserHomeDir()
			if err == nil {
				keyPath = filepath.Join(home, keyPath[2:])
			}
		}

		key, err := os.ReadFile(keyPath)
		if err != nil {
			log.Errorf("读取SSH私钥失败: %v", err)
			return nil, err
		}

		signer, err := ssh.ParsePrivateKey(key)
		if err != nil {
			log.Errorf("解析SSH私钥失败: %v", err)
			return nil, err
		}

		log.Debug("成功加载SSH私钥")
		authMethods = append(authMethods, ssh.PublicKeys(signer))
	} else {
		password, err := utils.GetSSHPassword(cfg.SSHPassword, cfg.InteractiveAuth, cfg.SSHUser, cfg.SSHServer)
		if err != nil {
			return nil, fmt.Errorf("获取SSH密码失败：%v", err)
		}
		log.Debug("使用密码认证")
		authMethods = append(authMethods, ssh.Password(password))
	}

	sshConfig := &ssh.ClientConfig{
		User:            cfg.SSHUser,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         cfg.Timeout,
	}

	log.Infof("正在连接SSH服务器 %s...", cfg.SSHServer)
	client, err := ssh.Dial("tcp", cfg.SSHServer, sshConfig)
	if err != nil {
		log.Errorf("SSH连接失败: %v", err)
		return nil, err
	}

	log.Info("成功建立SSH连接")

	return &SSHClient{
		client: client,
		config: sshConfig,
		logger: log,
	}, nil
}

// Dial 通过SSH隧道连接到目标服务器
func (s *SSHClient) Dial(network, addr string) (net.Conn, error) {
	s.logger.Debugf("通过SSH隧道连接到 %s://%s", network, addr)
	conn, err := s.client.Dial(network, addr)
	if err != nil {
		s.logger.Errorf("通过SSH隧道连接 %s 失败: %v", addr, err)
		return nil, err
	}
	return conn, nil
}

// Close 关闭SSH连接
func (s *SSHClient) Close() error {
	if s.client != nil {
		s.logger.Debug("关闭SSH连接")
		return s.client.Close()
	}
	return nil
}
