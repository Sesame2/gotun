package proxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

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
	if cfg.Context != nil && cfg.Context.Err() != nil {
		return nil, cfg.Context.Err()
	}
	hostKeyCallback, err := knownHostCallback(cfg)
	if err != nil {
		return nil, err
	}
	sshClient := &SSHClient{
		logger:      log,
		jumpClients: []*ssh.Client{},
	}

	// 优先使用结构化配置 (GUI)
	if len(cfg.JumpHostsList) > 0 {
		for i, jh := range cfg.JumpHostsList {
			addr := net.JoinHostPort(jh.Host, jh.Port)
			log.Infof("准备连接跳板机 (GUI) %d/%d: %s", i+1, len(cfg.JumpHostsList), addr)

			var lastClient *ssh.Client
			if len(sshClient.jumpClients) > 0 {
				lastClient = sshClient.jumpClients[len(sshClient.jumpClients)-1]
			}

			client, err := connectToHost(cfg, log, jh.User, jh.Password, addr, lastClient, hostKeyCallback)
			if err != nil {
				log.Errorf("连接跳板机 %s 失败: %v", addr, err)
				sshClient.Close()
				return nil, err
			}
			sshClient.jumpClients = append(sshClient.jumpClients, client)
			log.Infof("已连接跳板机 %d: %s@%s", i+1, jh.User, addr)
		}
	} else {
		// 否则尝试使用字符串列表 (CLI Legacy)
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
			addr := net.JoinHostPort(host, port)
			log.Infof("准备连接跳板机 %d/%d: %s", i+1, len(cfg.JumpHosts), addr)

			var lastClient *ssh.Client
			if len(sshClient.jumpClients) > 0 {
				lastClient = sshClient.jumpClients[len(sshClient.jumpClients)-1]
			}

			// CLI模式下跳板机通常共用主密码或使用密钥
			client, err := connectToHost(cfg, log, user, cfg.SSHPassword, addr, lastClient, hostKeyCallback)
			if err != nil {
				log.Errorf("连接跳板机 %s 失败: %v", addr, err)
				sshClient.Close()
				return nil, err
			}
			sshClient.jumpClients = append(sshClient.jumpClients, client)
			log.Infof("已连接跳板机 %d: %s@%s", i+1, user, addr)
		}
	}

	// 准备连接最终目标服务器
	log.Infof("准备连接目标服务器: %s", cfg.SSHServer)
	var lastJumpClient *ssh.Client
	if len(sshClient.jumpClients) > 0 {
		lastJumpClient = sshClient.jumpClients[len(sshClient.jumpClients)-1]
	}

	finalClient, err := connectToHost(cfg, log, cfg.SSHUser, cfg.SSHPassword, cfg.SSHServer, lastJumpClient, hostKeyCallback)
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
func connectToHost(cfg *config.Config, log *logger.Logger, user, password, addr string, jumpVia *ssh.Client, hostKeyCallback ssh.HostKeyCallback) (*ssh.Client, error) {
	ctx := cfg.Context
	if ctx == nil {
		ctx = context.Background()
	}
	attempt := func(auths []ssh.AuthMethod) (*ssh.Client, error) {
		connect := func() (*ssh.Client, error) {
			networkCtx := ctx
			if cfg.Timeout > 0 {
				var cancel context.CancelFunc
				networkCtx, cancel = context.WithTimeout(ctx, cfg.Timeout)
				defer cancel()
			}
			return trySingleConnection(networkCtx, user, addr, auths, jumpVia, hostKeyCallback)
		}
		client, err := connect()
		var verification *hostKeyVerificationError
		if errors.As(err, &verification) && verification.confirm != nil {
			if err := verification.confirm(ctx); err != nil {
				return nil, err
			}
			// Trust was pinned before reconnecting; a different key is now a hard mismatch.
			return connect()
		}
		return client, err
	}
	var authErr error
	// 阶段一：仅尝试私钥认证
	log.Debugf("阶段 1: 尝试使用私钥连接 %s", addr)
	keyAuthCfg := &AuthConfig{User: user, ServerAddr: addr, KeyFile: cfg.SSHKeyFile}
	keyAuths, err := getAuthMethods(keyAuthCfg, log, false) // false表示获取私钥
	if err == nil && len(keyAuths) > 0 {
		client, err := attempt(keyAuths)
		if err == nil {
			log.Debugf("私钥认证成功: %s", addr)
			return client, nil // 私钥成功，直接返回
		}
		var verification *hostKeyVerificationError
		if errors.As(err, &verification) {
			return nil, err
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		authErr = err
		log.Warnf("私钥认证失败: %v。将尝试其他方法...", err)
	} else if err != nil {
		authErr = err
		log.Debugf("获取私钥方法时出错: %v", err)
	}

	// 阶段二：如果私钥失败，并且配置了密码/交互模式，则尝试它们
	if cfg.InteractiveAuth || password != "" {
		log.Debugf("阶段 2: 尝试使用密码/交互式认证连接 %s", addr)
		passwordAuthCfg := &AuthConfig{User: user, ServerAddr: addr, Password: password, InteractiveAuth: cfg.InteractiveAuth}
		passwordAuths, err := getAuthMethods(passwordAuthCfg, log, true) // true表示仅获取密码
		if err == nil && len(passwordAuths) > 0 {
			client, err := attempt(passwordAuths)
			if err == nil {
				log.Debugf("密码/交互式认证成功: %s", addr)
				return client, nil
			}
			var verification *hostKeyVerificationError
			if errors.As(err, &verification) {
				return nil, err
			}
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			authErr = err
			log.Warnf("密码/交互式认证失败: %v", err)
		} else if err != nil {
			authErr = err
			log.Debugf("获取密码方法时出错: %v", err)
		}
	}

	return nil, fmt.Errorf("所有认证方法均失败: %w", authErr)
}

// trySingleConnection 尝试使用给定的认证方法进行一次连接
func trySingleConnection(ctx context.Context, user, addr string, auths []ssh.AuthMethod, jumpVia *ssh.Client, hostKeyCallback ssh.HostKeyCallback) (*ssh.Client, error) {
	sshConfig := &ssh.ClientConfig{
		User:            user,
		Auth:            auths,
		HostKeyCallback: hostKeyCallback,
	}
	var conn net.Conn
	var err error
	if jumpVia == nil {
		conn, err = (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = jumpVia.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return nil, fmt.Errorf("SSH TCP connection to %s: %w", addr, err)
	}
	type handshakeResult struct {
		client *ssh.Client
		err    error
	}
	result := make(chan handshakeResult)
	go func() {
		c, chans, reqs, err := ssh.NewClientConn(conn, addr, sshConfig)
		var client *ssh.Client
		if err == nil {
			client = ssh.NewClient(c, chans, reqs)
		}
		select {
		case result <- handshakeResult{client, err}:
		case <-ctx.Done():
			if client != nil {
				client.Close()
			}
		}
	}()
	select {
	case <-ctx.Done():
		// SSH channel connections do not implement SetDeadline; closing also interrupts jump-host handshakes.
		conn.Close()
		return nil, ctx.Err()
	case res := <-result:
		if res.err != nil {
			conn.Close()
			return nil, fmt.Errorf("SSH handshake with %s: %w", addr, res.err)
		}
		return res.client, nil
	}
}

type hostKeyVerificationError struct {
	error
	confirm func(context.Context) error
}

func (e *hostKeyVerificationError) Unwrap() error { return e.error }

var knownHostsMu sync.Mutex

func readKnownHosts(path string) (ssh.HostKeyCallback, error) {
	check, err := knownhosts.New(path)
	if os.IsNotExist(err) {
		return func(string, net.Addr, ssh.PublicKey) error { return &knownhosts.KeyError{} }, nil
	}
	return check, err
}

func knownHostCallback(cfg *config.Config) (ssh.HostKeyCallback, error) {
	path := cfg.KnownHostsFile
	var ownerReference string
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("locate SSH known_hosts: %w", err)
		}
		path = filepath.Join(home, ".ssh", "known_hosts")
		ownerReference = home
	}
	reference, err := utils.OwnerReference(path)
	if err != nil {
		return nil, fmt.Errorf("locate SSH known_hosts owner: %w", err)
	}
	if ownerReference == "" {
		ownerReference = reference
	}
	check, err := readKnownHosts(path)
	if err != nil {
		return nil, fmt.Errorf("read SSH known_hosts %s: %w", path, err)
	}
	return func(host string, remote net.Addr, key ssh.PublicKey) error {
		err := check(host, remote, key)
		if err == nil {
			return nil
		}
		failure := func(err error) *hostKeyVerificationError {
			return &hostKeyVerificationError{error: fmt.Errorf("SSH host key verification failed for %s (%s); verify the server fingerprint and update known_hosts %s: %w", host, ssh.FingerprintSHA256(key), path, err)}
		}
		verification := failure(err)
		var unknown *knownhosts.KeyError
		if !errors.As(err, &unknown) || len(unknown.Want) > 0 || cfg.HostKeyPrompt == nil {
			return verification
		}
		verification.confirm = func(parent context.Context) error {
			ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
			defer cancel()
			confirmed := make(chan bool)
			go func() {
				accepted := cfg.HostKeyPrompt(host, ssh.FingerprintSHA256(key))
				select {
				case confirmed <- accepted:
				case <-ctx.Done():
				}
			}()
			select {
			case <-ctx.Done():
				return failure(ctx.Err())
			case accepted := <-confirmed:
				if !accepted {
					return verification
				}
			}
			if ctx.Err() != nil {
				return failure(ctx.Err())
			}
			knownHostsMu.Lock()
			defer knownHostsMu.Unlock()
			// Recheck after the dialog so another connection cannot replace an existing key.
			check, err = readKnownHosts(path)
			if err != nil {
				return failure(err)
			}
			if err = check(host, remote, key); err == nil {
				return nil
			}
			if !errors.As(err, &unknown) || len(unknown.Want) > 0 {
				return failure(err)
			}
			if _, err := os.Stat(filepath.Dir(path)); os.IsNotExist(err) || cfg.KnownHostsFile == "" {
				if err := utils.EnsurePrivateDir(filepath.Dir(path), ownerReference); err != nil {
					return failure(err)
				}
			} else if err != nil {
				return failure(err)
			}
			file, err := utils.OpenPrivateFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, ownerReference)
			if err != nil {
				return failure(err)
			}
			defer file.Close()
			if _, err := file.WriteString("\n" + knownhosts.Line([]string{host}, key) + "\n"); err != nil {
				return failure(err)
			}
			if err := file.Sync(); err != nil {
				return failure(err)
			}
			if err := file.Close(); err != nil {
				return failure(err)
			}
			check, err = readKnownHosts(path)
			if err != nil {
				return failure(err)
			}
			return nil
		}
		return verification
	}, nil
}

// RemoteAddr identifies the physical SSH peer, before any jump-host forwarding.
func (s *SSHClient) RemoteAddr() net.Addr {
	if len(s.jumpClients) > 0 {
		return s.jumpClients[0].RemoteAddr()
	}
	if s.client != nil {
		return s.client.RemoteAddr()
	}
	return nil
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

// 增加Dial方法的实现，使其满足常见的 Dialer 接口
func (s *SSHClient) Dial(network, addr string) (net.Conn, error) {
	if s.client == nil {
		return nil, fmt.Errorf("ssh client not ready")
	}
	return s.client.Dial(network, addr)
}
