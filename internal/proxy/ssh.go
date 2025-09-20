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

	// 尝试所有可用私钥（如果未显式指定）
	if cfg.SSHKeyFile != "" {
		log.Infof("用户指定了SSH私钥: %s", cfg.SSHKeyFile)
		signer, err := loadPrivateKey(cfg.SSHKeyFile)
		if err != nil {
			return nil, fmt.Errorf("加载指定SSH私钥失败: %v", err)
		}
		return trySSHConnection(cfg, log, []ssh.AuthMethod{ssh.PublicKeys(signer)})
	} else {
		// 搜索默认目录 ~/.ssh 下的所有常见私钥
		home, _ := os.UserHomeDir()
		keyDir := filepath.Join(home, ".ssh")
		candidateKeys := []string{"id_rsa", "id_ed25519", "id_ecdsa", "id_dsa"}

		for _, name := range candidateKeys {
			keyPath := filepath.Join(keyDir, name)
			signer, err := loadPrivateKey(keyPath)
			if err != nil {
				log.Debugf("跳过不可用私钥 %s: %v", keyPath, err)
				continue
			}

			log.Infof("尝试使用私钥: %s", keyPath)
			client, err := trySSHConnection(cfg, log, []ssh.AuthMethod{ssh.PublicKeys(signer)})
			if err == nil {
				log.Infof("使用私钥成功连接: %s", keyPath)
				return client, nil
			}
			log.Warnf("私钥连接失败: %v", err)
		}
	}

	// 所有私钥失败，尝试交互式密码认证（如启用）
	if cfg.InteractiveAuth {
		password, err := utils.GetSSHPassword(cfg.SSHPassword, cfg.InteractiveAuth, cfg.SSHUser, cfg.SSHServer)
		if err != nil {
			return nil, fmt.Errorf("获取SSH密码失败：%v", err)
		}
		log.Infof("尝试密码认证连接 %s...", cfg.SSHServer)
		return trySSHConnection(cfg, log, []ssh.AuthMethod{ssh.Password(password)})
	}

	return nil, fmt.Errorf("未能使用私钥连接成功，且未启用交互式密码认证")
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

func loadPrivateKey(path string) (ssh.Signer, error) {
	expanded := path
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("无法展开 ~ 路径: %v", err)
		}
		expanded = filepath.Join(home, path[2:])
	}

	key, err := os.ReadFile(expanded)
	if err != nil {
		return nil, fmt.Errorf("读取私钥文件失败: %v", err)
	}

	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("解析私钥失败: %v", err)
	}

	return signer, nil
}

func trySSHConnection(cfg *config.Config, log *logger.Logger, auths []ssh.AuthMethod) (*SSHClient, error) {
	sshConfig := &ssh.ClientConfig{
		User:            cfg.SSHUser,
		Auth:            auths,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         cfg.Timeout,
	}

	log.Infof("连接SSH服务器 %s...", cfg.SSHServer)
	client, err := ssh.Dial("tcp", cfg.SSHServer, sshConfig)
	if err != nil {
		log.Warnf("SSH连接失败: %v", err)
		return nil, err
	}

	log.Infof("成功建立SSH连接")
	return &SSHClient{
		client: client,
		config: sshConfig,
		logger: log,
	}, nil
}
