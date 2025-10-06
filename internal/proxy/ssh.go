package proxy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/Sesame2/gotun/internal/config"
	"github.com/Sesame2/gotun/internal/logger"
	"github.com/Sesame2/gotun/internal/utils"
)

// SSHClient 管理SSH连接
type SSHClient struct {
	client      *ssh.Client   // 这个是最终目标机器的连接
	jumpClients []*ssh.Client // 这里存储所有跳板机的连接
	config      *ssh.ClientConfig
	logger      *logger.Logger
}

type AuthConfig struct {
	User            string
	Password        string
	KeyFile         string
	ServerAddr      string
	InteractiveAuth bool
}

// getAuthMethods 根据配置生成ssh.AuthMethod列表
func getAuthMethods(authCfg *AuthConfig, log *logger.Logger, passwordOnly bool) ([]ssh.AuthMethod, error) {
	var authMethods []ssh.AuthMethod

	if !passwordOnly {
		// 优先使用指定的私钥文件
		if authCfg.KeyFile != "" {
			log.Debugf("尝试使用指定的SSH私钥: %s", authCfg.KeyFile)
			signer, err := loadPrivateKey(authCfg.KeyFile)
			if err != nil {
				return nil, fmt.Errorf("加载指定SSH私钥失败: %v", err)
			}
			authMethods = append(authMethods, ssh.PublicKeys(signer))
		} else {
			// 否则，尝试所有默认位置的私钥
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
				log.Debugf("找到并添加默认私钥进行尝试: %s", keyPath)
				authMethods = append(authMethods, ssh.PublicKeys(signer))
			}
		}
	}

	// 如果启用了密码或交互式认证，则添加密码认证方法
	if passwordOnly {
		password, err := utils.GetSSHPassword(authCfg.Password, authCfg.InteractiveAuth, authCfg.User, authCfg.ServerAddr)
		if err != nil {
			return nil, fmt.Errorf("获取SSH密码失败: %v", err)
		}
		authMethods = append(authMethods, ssh.Password(password))
	}

	if len(authMethods) == 0 {
		if passwordOnly {
			return nil, fmt.Errorf("未配置密码或交互式认证")
		}
		return nil, fmt.Errorf("未找到可用的SSH私钥")
	}
	return authMethods, nil
}

func NewSSHClient(cfg *config.Config, log *logger.Logger) (*SSHClient, error) {
	sshClient := &SSHClient{
		logger:      log,
		jumpClients: []*ssh.Client{},
	}

	// 尝试连接所有跳板机
	for i, jumpHostsStr := range cfg.JumpHosts {
		user, host, port, err := cfg.GetJumpHostInfo(jumpHostsStr)
		if err != nil {
			log.Errorf("跳板机参数解析失败: %v", err)
			sshClient.Close()
			return nil, err
		}
		if user == "" {
			user = cfg.SSHUser
		}
		addr := fmt.Sprintf("%s:%s", host, port)
		log.Infof("准备连接跳板机 %d/%d: %s", i+1, len(cfg.JumpHosts), addr)

		var lastClient *ssh.Client
		if len(sshClient.jumpClients) > 0 {
			lastClient = sshClient.jumpClients[len(sshClient.jumpClients)-1]
		}

		client, err := connectToHost(cfg, log, user, addr, lastClient)
		if err != nil {
			log.Errorf("连接跳板机 %s 失败: %v", addr, err)
			sshClient.Close()
			return nil, err
		}
		sshClient.jumpClients = append(sshClient.jumpClients, client)
		log.Infof("已连接跳板机 %d: %s@%s", i+1, user, addr)
	}

	// 准备连接最终目标服务器
	log.Infof("准备连接目标服务器: %s", cfg.SSHServer)
	var lastJumpClient *ssh.Client
	if len(sshClient.jumpClients) > 0 {
		lastJumpClient = sshClient.jumpClients[len(sshClient.jumpClients)-1]
	}

	finalClient, err := connectToHost(cfg, log, cfg.SSHUser, cfg.SSHServer, lastJumpClient)
	if err != nil {
		log.Errorf("连接目标服务器 %s 失败: %v", cfg.SSHServer, err)
		sshClient.Close()
		return nil, err
	}

	sshClient.client = finalClient
	log.Infof("已连接到目标服务器: %s", cfg.SSHServer)
	return sshClient, nil
}

// connectToHost 封装了连接单个主机（跳板机或最终目标）的完整逻辑
func connectToHost(cfg *config.Config, log *logger.Logger, user, addr string, jumpVia *ssh.Client) (*ssh.Client, error) {
	// 阶段一：仅尝试私钥认证
	log.Debugf("阶段 1: 尝试使用私钥连接 %s", addr)
	keyAuthCfg := &AuthConfig{User: user, ServerAddr: addr, KeyFile: cfg.SSHKeyFile}
	keyAuths, err := getAuthMethods(keyAuthCfg, log, false) // false表示获取私钥
	if err == nil && len(keyAuths) > 0 {
		client, err := trySingleConnection(user, addr, cfg.Timeout, keyAuths, jumpVia)
		if err == nil {
			log.Debugf("私钥认证成功: %s", addr)
			return client, nil // 私钥成功，直接返回
		}
		log.Warnf("私钥认证失败: %v。将尝试其他方法...", err)
	} else if err != nil {
		log.Debugf("获取私钥方法时出错: %v", err)
	}

	// 阶段二：如果私钥失败，并且配置了密码/交互模式，则尝试它们
	if cfg.InteractiveAuth || cfg.SSHPassword != "" {
		log.Debugf("阶段 2: 尝试使用密码/交互式认证连接 %s", addr)
		passwordAuthCfg := &AuthConfig{User: user, ServerAddr: addr, Password: cfg.SSHPassword, InteractiveAuth: cfg.InteractiveAuth}
		passwordAuths, err := getAuthMethods(passwordAuthCfg, log, true) // true表示仅获取密码
		if err == nil && len(passwordAuths) > 0 {
			client, err := trySingleConnection(user, addr, cfg.Timeout, passwordAuths, jumpVia)
			if err == nil {
				log.Debugf("密码/交互式认证成功: %s", addr)
				return client, nil
			}
			log.Warnf("密码/交互式认证失败: %v", err)
		} else if err != nil {
			log.Debugf("获取密码方法时出错: %v", err)
		}
	}

	return nil, fmt.Errorf("所有认证方法均失败")
}

// trySingleConnection 尝试使用给定的认证方法进行一次连接
func trySingleConnection(user, addr string, timeout time.Duration, auths []ssh.AuthMethod, jumpVia *ssh.Client) (*ssh.Client, error) {
	sshConfig := &ssh.ClientConfig{
		User:            user,
		Auth:            auths,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         timeout,
	}

	if jumpVia == nil {
		// 直接连接
		return ssh.Dial("tcp", addr, sshConfig)
	}

	// 通过跳板机连接
	conn, err := jumpVia.Dial("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("通过跳板机隧道连接到 %s 失败: %v", addr, err)
	}
	c, chans, reqs, err := ssh.NewClientConn(conn, addr, sshConfig)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("在跳板机隧道上建立SSH连接到 %s 失败: %v", addr, err)
	}
	return ssh.NewClient(c, chans, reqs), nil
}

// Close 关闭所有连接（逆序关闭跳板机）
func (s *SSHClient) Close() error {
	if s.client != nil {
		s.logger.Debug("关闭目标SSH连接")
		s.client.Close()
	}
	if s.jumpClients != nil {
		for i := len(s.jumpClients) - 1; i >= 0; i-- {
			if s.jumpClients[i] != nil {
				s.logger.Debugf("关闭跳板机连接 %d", i+1)
				s.jumpClients[i].Close()
			}
		}
	}
	s.jumpClients = nil
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
		return nil, fmt.Errorf("读取私钥文件 '%s' 失败: %v", expanded, err)
	}

	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		// 为密码保护的私钥提供更友好的错误提示
		if _, ok := err.(*ssh.PassphraseMissingError); ok {
			return nil, fmt.Errorf("私钥 '%s' 受密码保护，暂不支持自动处理", expanded)
		}
		return nil, fmt.Errorf("解析私钥文件 '%s' 失败: %v", expanded, err)
	}

	return signer, nil
}
