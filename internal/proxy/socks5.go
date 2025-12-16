package proxy

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/Sesame2/gotun/internal/config"
	"github.com/Sesame2/gotun/internal/logger"
	"github.com/Sesame2/gotun/internal/router"
	"github.com/armon/go-socks5"
)

type SOCKS5OverSSH struct {
	server   *socks5.Server
	cfg      *config.Config
	logger   *logger.Logger
	listener net.Listener
}

// SSHDialer 这是一个实现了 proxy.Dialer 接口的结构体
// 它负责把 SOCKS5 库的拨号请求“劫持”到我们的逻辑里
type SSHDialer struct {
	ssh    *SSHClient
	router *router.Router
	logger *logger.Logger
}

// Dial 实现 DialContext 接口
func (d *SSHDialer) Dial(ctx context.Context, network, addr string) (net.Conn, error) {
	// 1. 路由判断
	// FIXME: 使用 SOCKS5 时，建议客户端开启 "Proxy DNS when using SOCKS5" 或类似选项，以确保域名规则生效。
	if d.router != nil {
		host, _, _ := net.SplitHostPort(addr)
		action := d.router.Match(host)

		// 如果规则是直连
		if action == router.ActionDirect {
			d.logger.Infof("[SOCKS5] 规则匹配: %s -> DIRECT", host)
			// 使用本地网络直连
			dialer := net.Dialer{Timeout: 10 * time.Second}
			return dialer.DialContext(ctx, network, addr)
		}
		d.logger.Infof("[SOCKS5] 规则匹配: %s -> PROXY", host)
	}
	// 2. 默认走 SSH 代理
	d.logger.Debugf("[SOCKS5] SSH Tunneling to %s", addr)

	// ssh.Client.Dial 没有 Context 参数，这里忽略 ctx
	return d.ssh.Dial(network, addr)
}

// NewSOCKS5OverSSH 创建 SOCKS5 代理
// 注意：这里传入已经建立好的 sshClient
func NewSOCKS5OverSSH(cfg *config.Config, log *logger.Logger, sshClient *SSHClient, r *router.Router) (*SOCKS5OverSSH, error) {
	// 创建自定义 Dialer
	sshDialer := &SSHDialer{
		ssh:    sshClient,
		router: r,
		logger: log,
	}

	// 配置 socks5 库
	conf := &socks5.Config{
		Dial:   sshDialer.Dial, // 注入我们的拨号逻辑
		Logger: nil,            // fixme: 注入日志（需要适配接口，或者置为 nil 自己打日志）
	}

	server, err := socks5.New(conf)
	if err != nil {
		return nil, fmt.Errorf("创建 SOCKS5 server 失败: %v", err)
	}

	return &SOCKS5OverSSH{
		server: server,
		cfg:    cfg,
		logger: log,
	}, nil
}

// Start 启动监听
func (s *SOCKS5OverSSH) Start() error {
	address := s.cfg.SocksAddr // 需要在 Config 里加这个字段
	if address == "" {
		return nil // 没配地址就不启动
	}

	l, err := net.Listen("tcp", address) // <--- 手动创建 Listener
	if err != nil {
		return err
	}
	s.listener = l // <--- 保存 Listener
	s.logger.Infof("SOCKS5 代理服务器启动在 %s", address)
	return s.server.Serve(l)
}

// Close 关闭 SOCKS5 代理服务
func (s *SOCKS5OverSSH) Close() error {
	if s.listener != nil {
		return s.listener.Close()
	}
	return nil
}
