package proxy

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/Sesame2/gotun/internal/config"
	"github.com/Sesame2/gotun/internal/logger"
	"github.com/Sesame2/gotun/internal/router"
)

type SOCKS5OverSSH struct {
	cfg      *config.Config
	logger   *logger.Logger
	ssh      *SSHClient
	router   *router.Router
	listener net.Listener
	mu       sync.Mutex // 互斥锁，保证 Close 的线程安全
}

// NewSOCKS5OverSSH 创建 SOCKS5 代理实例
func NewSOCKS5OverSSH(cfg *config.Config, log *logger.Logger, sshClient *SSHClient, r *router.Router) (*SOCKS5OverSSH, error) {
	return &SOCKS5OverSSH{
		cfg:    cfg,
		logger: log,
		ssh:    sshClient,
		router: r,
	}, nil
}

// Start 启动 SOCKS5 监听循环
func (s *SOCKS5OverSSH) Start() error {
	addr := s.cfg.SocksAddr
	if addr == "" {
		s.logger.Debug("SOCKS5 代理地址未配置，跳过启动")
		return nil
	}

	l, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("SOCKS5 监听启动失败: %w", err)
	}

	s.mu.Lock()
	s.listener = l
	s.mu.Unlock()

	s.logger.Infof("SOCKS5 代理已启动，监听地址: %s", addr)

	for {
		conn, err := l.Accept()
		if err != nil {
			s.mu.Lock()
			closing := s.listener == nil
			s.mu.Unlock()

			if closing {
				return nil // 正常退出
			}
			s.logger.Errorf("SOCKS5 Accept 错误: %v", err)
			continue
		}

		// 异步处理每个连接
		go s.handleConnection(conn)
	}
}

// Close 优雅关闭服务
func (s *SOCKS5OverSSH) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.listener == nil {
		return nil
	}

	s.logger.Info("正在关闭 SOCKS5 代理服务...")
	err := s.listener.Close()
	s.listener = nil // 置空，防止重复关闭
	return err
}

// handleConnection 处理单个 SOCKS5 连接的主逻辑
func (s *SOCKS5OverSSH) handleConnection(conn net.Conn) {
	defer conn.Close()
	clientAddr := conn.RemoteAddr().String()

	// 1. 协商阶段 (Handshake)
	// Client: [VER, NMETHODS, METHODS...]
	// Server: [VER, METHOD]
	if err := s.handshake(conn); err != nil {
		s.logger.Debugf("[%s] SOCKS5 协商失败: %v", clientAddr, err)
		return
	}

	// 2. 请求阶段 (Request)
	// Client: [VER, CMD, RSV, ATYP, DST.ADDR, DST.PORT]
	targetAddr, hostForRoute, err := s.readRequest(conn)
	if err != nil {
		s.logger.Debugf("[%s] SOCKS5 请求解析失败: %v", clientAddr, err)
		return
	}

	// 3. 路由与连接 (Dial)
	start := time.Now()
	destConn, ruleAction, err := s.dialTarget(targetAddr, hostForRoute)
	if err != nil {
		s.logger.Warnf("[%SOCKS5] 连接目标 %s 失败: %v", targetAddr, err)
		s.reply(conn, 0x05) // 0x05: Connection refused
		return
	}
	defer destConn.Close()

	// 4. 回复客户端成功 (Reply Success)
	// Server: [VER, REP, RSV, ATYP, BND.ADDR, BND.PORT]
	s.reply(conn, 0x00) // 0x00: Succeeded

	s.logger.Infof("[SOCKS5] 建立连接 -> %s (规则: %s)", targetAddr, ruleAction)

	// 5. 数据传输 (Transfer)
	var wg sync.WaitGroup
	wg.Add(2)

	// Browser -> SSH/Target
	go func() {
		defer wg.Done()
		io.Copy(destConn, conn)
		// 如果是 TCP 连接，通常不需要手动 CloseWrite，但在某些场景下可以加速关闭
		if c, ok := destConn.(*net.TCPConn); ok {
			c.CloseWrite()
		}
	}()

	// SSH/Target -> Browser
	go func() {
		defer wg.Done()
		io.Copy(conn, destConn)
		if c, ok := conn.(*net.TCPConn); ok {
			c.CloseWrite()
		}
	}()

	wg.Wait()
	s.logger.Debugf("[%s] 连接断开: %s, 耗时: %v", clientAddr, targetAddr, time.Since(start))
}

// handshake 处理 SOCKS5 认证协商
func (s *SOCKS5OverSSH) handshake(conn net.Conn) error {
	buf := make([]byte, 256)
	// 读取: VER, NMETHODS
	if _, err := io.ReadFull(conn, buf[:2]); err != nil {
		return err
	}
	if buf[0] != 0x05 {
		return fmt.Errorf("不支持的 SOCKS 版本: %d", buf[0])
	}
	nmethods := int(buf[1])
	// 读取: METHODS
	if _, err := io.ReadFull(conn, buf[:nmethods]); err != nil {
		return err
	}

	// 回复: VER=5, METHOD=0 (No Authentication Required)
	_, err := conn.Write([]byte{0x05, 0x00})
	return err
}

// readRequest 读取并解析客户端请求，返回完整目标地址(host:port)和用于路由匹配的主机名
func (s *SOCKS5OverSSH) readRequest(conn net.Conn) (string, string, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(conn, header); err != nil {
		return "", "", err
	}

	if header[1] != 0x01 { // CMD: 0x01 = CONNECT
		s.reply(conn, 0x07) // Command not supported
		return "", "", fmt.Errorf("不支持的命令: %d", header[1])
	}

	var host string
	switch header[3] { // ATYP
	case 0x01: // IPv4
		ip := make([]byte, 4)
		if _, err := io.ReadFull(conn, ip); err != nil {
			return "", "", err
		}
		host = net.IP(ip).String()
	case 0x03: // Domain Name (关键：直接读取域名，不进行本地 DNS 解析)
		lenBuf := make([]byte, 1)
		if _, err := io.ReadFull(conn, lenBuf); err != nil {
			return "", "", err
		}
		domainLen := int(lenBuf[0])
		domain := make([]byte, domainLen)
		if _, err := io.ReadFull(conn, domain); err != nil {
			return "", "", err
		}
		host = string(domain)
	case 0x04: // IPv6
		ip := make([]byte, 16)
		if _, err := io.ReadFull(conn, ip); err != nil {
			return "", "", err
		}
		host = net.IP(ip).String()
	default:
		s.reply(conn, 0x08) // Address type not supported
		return "", "", fmt.Errorf("不支持的地址类型: %d", header[3])
	}

	portBuf := make([]byte, 2)
	if _, err := io.ReadFull(conn, portBuf); err != nil {
		return "", "", err
	}
	port := binary.BigEndian.Uint16(portBuf)

	targetAddr := net.JoinHostPort(host, strconv.Itoa(int(port)))
	return targetAddr, host, nil
}

// dialTarget 根据路由规则连接目标
func (s *SOCKS5OverSSH) dialTarget(addr string, hostForRoute string) (net.Conn, string, error) {
	action := router.ActionProxy

	// 1. 路由判断
	if s.router != nil {
		action = s.router.Match(hostForRoute)
	}

	// 2. 根据动作执行连接
	if action == router.ActionDirect {
		s.logger.Debugf("[SOCKS5] 路由直连: %s", addr)
		conn, err := net.DialTimeout("tcp", addr, s.cfg.Timeout)
		return conn, "DIRECT", err
	}

	// 默认走 Proxy (SSH)
	// SSH 服务器会在远端进行 DNS 解析，从而解决本地 DNS 污染和 HSTS 问题
	s.logger.Debugf("[SOCKS5] SSH 转发: %s", addr)
	conn, err := s.ssh.Dial("tcp", addr)
	return conn, "PROXY", err
}

// reply 发送 SOCKS5 响应包
func (s *SOCKS5OverSSH) reply(conn net.Conn, rep byte) {
	// [VER, REP, RSV, ATYP, BND.ADDR(4), BND.PORT(2)]
	// 这里 BND.ADDR 和 BND.PORT 填全 0 即可，客户端通常不关心
	conn.Write([]byte{0x05, rep, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
}
