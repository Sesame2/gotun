package proxy

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Sesame2/gotun/internal/config"
	"github.com/Sesame2/gotun/internal/logger"
	"github.com/Sesame2/gotun/internal/router"
)

// HTTPOverSSH 表示基于SSH的HTTP代理
type HTTPOverSSH struct {
	cfg         *config.Config
	ssh         *SSHClient
	logger      *logger.Logger
	server      *http.Server
	listener    net.Listener
	router      *router.Router
	connections connectionSet
}

// NewHTTPOverSSH 创建HTTP代理
func NewHTTPOverSSH(cfg *config.Config, log *logger.Logger, sshClient *SSHClient, r *router.Router) (*HTTPOverSSH, error) {
	log.Info("初始化HTTP-over-SSH代理")

	proxy := &HTTPOverSSH{
		cfg:    cfg,
		ssh:    sshClient,
		logger: log,
		router: r,
	}

	listener, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		return nil, fmt.Errorf("HTTP 监听启动失败: %w", err)
	}
	proxy.listener = listener
	proxy.server = &http.Server{Handler: http.HandlerFunc(proxy.handleHTTP)}
	return proxy, nil
}

// Start 启动HTTP代理服务
func (p *HTTPOverSSH) Start() error {
	p.logger.Infof("HTTP/HTTPS 代理服务器启动在 %s", p.cfg.ListenAddr)
	err := p.server.Serve(p.listener)
	if err == http.ErrServerClosed || errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

// handleHTTP 处理所有传入请求的入口
func (p *HTTPOverSSH) handleHTTP(w http.ResponseWriter, req *http.Request) {
	req = req.Clone(req.Context())
	req.RequestURI = ""
	stripHopHeaders(req.Header)
	if req.Method == http.MethodConnect {
		p.handleHTTPSConnect(w, req)
	} else {
		p.handlePlainHTTP(w, req)
	}
}

// handlePlainHTTP 处理非CONNECT的HTTP请求
func (p *HTTPOverSSH) handlePlainHTTP(w http.ResponseWriter, req *http.Request) {
	startTime := time.Now()

	// 路由判断
	if p.router != nil {
		action := p.router.Match(req.Host)
		if action == router.ActionReject {
			http.Error(w, "路由规则拒绝此请求", http.StatusForbidden)
			return
		}
		if action == router.ActionDirect {
			p.logger.Infof("规则匹配: %s -> DIRECT", req.Host)
			p.handleDirect(w, req)
			return
		}
		p.logger.Infof("规则匹配: %s -> PROXY", req.Host)
	}

	if !req.URL.IsAbs() {
		p.logger.Warnf("收到非绝对URL请求: %s", req.URL.String())
		http.Error(w, "需要绝对URL", http.StatusBadRequest)
		return
	}

	p.logger.Infof("来自 %s 的请求: %s %s", req.RemoteAddr, req.Method, req.URL.String())

	targetAddr := req.URL.Host
	// 检查是否强制了上游转发
	if p.cfg.HTTPUpstream != "" {
		targetAddr = p.cfg.HTTPUpstream
		p.logger.Infof("Upstream forced to %s", targetAddr)
	}

	if !strings.Contains(targetAddr, ":") {
		targetAddr += ":80"
	}

	conn, err := p.ssh.client.Dial("tcp", targetAddr)
	if err != nil {
		p.logger.Errorf("无法通过SSH连接到目标 %s: %v", targetAddr, err)
		http.Error(w, "无法通过SSH连接到目标", http.StatusBadGateway)
		return
	}
	defer conn.Close()
	if !p.connections.add(conn) {
		return
	}
	defer p.connections.remove(conn)

	err = req.Write(conn)
	if err != nil {
		p.logger.Errorf("写入请求到远程服务器失败: %v", err)
		http.Error(w, "写入请求到远程服务器失败", http.StatusInternalServerError)
		return
	}

	resp, err := http.ReadResponse(bufio.NewReader(conn), req)
	if err != nil {
		p.logger.Errorf("从远程服务器读取响应失败: %v", err)
		http.Error(w, "从远程服务器读取响应失败", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	for k, v := range resp.Header {
		for _, vv := range v {
			w.Header().Add(k, vv)
		}
	}

	w.WriteHeader(resp.StatusCode)
	written, err := io.Copy(w, resp.Body)
	if err != nil {
		p.logger.Errorf("写入响应到客户端失败: %v", err)
		return
	}

	duration := time.Since(startTime)
	p.logger.Infof("请求完成: %s %s - %d (%d bytes) - %v", req.Method, req.URL.String(), resp.StatusCode, written, duration)
}

// Connection can name additional headers that belong only to this proxy hop.
func stripHopHeaders(header http.Header) {
	for _, connection := range header.Values("Connection") {
		for _, name := range strings.Split(connection, ",") {
			header.Del(strings.TrimSpace(name))
		}
	}
	for _, name := range []string{"Connection", "Proxy-Connection", "Proxy-Authenticate", "Proxy-Authorization", "Keep-Alive", "TE", "Trailer", "Transfer-Encoding", "Upgrade"} {
		header.Del(name)
	}
}

// handleHTTPSConnect 处理CONNECT请求
func (p *HTTPOverSSH) handleHTTPSConnect(w http.ResponseWriter, req *http.Request) {
	startTime := time.Now()

	// 路由判断
	if p.router != nil {
		action := p.router.Match(req.Host)
		if action == router.ActionReject {
			http.Error(w, "路由规则拒绝此请求", http.StatusForbidden)
			return
		}
		if action == router.ActionDirect {
			p.logger.Infof("规则匹配: %s -> DIRECT (CONNECT)", req.Host)
			p.handleDirectConnect(w, req)
			return
		}
		p.logger.Infof("规则匹配: %s -> PROXY (CONNECT)", req.Host)
	}

	p.logger.Infof("HTTPS CONNECT 请求: %s", req.Host)

	targetAddr := req.Host
	if !strings.Contains(targetAddr, ":") {
		targetAddr = targetAddr + ":443"
	}

	sshConn, err := p.ssh.client.Dial("tcp", targetAddr)
	if err != nil {
		p.logger.Errorf("无法通过SSH连接到HTTPS目标 %s: %v", targetAddr, err)
		http.Error(w, "无法通过SSH连接到HTTPS目标", http.StatusBadGateway)
		return
	}
	defer sshConn.Close()
	if !p.connections.add(sshConn) {
		return
	}
	defer p.connections.remove(sshConn)

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		p.logger.Error("代理服务器不支持连接劫持")
		http.Error(w, "代理服务器不支持HTTPS连接", http.StatusInternalServerError)
		return
	}

	clientConn, buffered, err := hijacker.Hijack()
	if err != nil {
		p.logger.Errorf("连接劫持失败: %v", err)
		sshConn.Close()
		http.Error(w, "连接劫持失败", http.StatusServiceUnavailable)
		return
	}
	defer clientConn.Close()
	if !p.connections.add(clientConn) {
		return
	}
	defer p.connections.remove(clientConn)

	_, err = clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
	if err != nil {
		p.logger.Errorf("向客户端写入隧道建立响应失败: %v", err)
		clientConn.Close()
		sshConn.Close()
		return
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go transfer(&wg, sshConn, struct {
		io.Reader
		io.Closer
	}{buffered.Reader, clientConn}, "client->ssh", p.logger)
	go transfer(&wg, clientConn, sshConn, "ssh->client", p.logger)
	wg.Wait()

	duration := time.Since(startTime)
	p.logger.Infof("HTTPS隧道已关闭: %s, 持续时间: %v", req.Host, duration)
}

// Close 关闭代理服务
func (p *HTTPOverSSH) Close() error {
	p.logger.Info("正在关闭代理服务")
	p.connections.close()
	var err error
	if p.server != nil {
		// Serve may not have registered the bound listener yet.
		p.listener.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err = p.server.Shutdown(ctx)
		if errors.Is(err, net.ErrClosed) {
			err = nil
		}
	}
	return err
}

// --- 新增的直连处理函数 ---

// handleDirect 处理直连的HTTP请求
func (p *HTTPOverSSH) handleDirect(w http.ResponseWriter, req *http.Request) {
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	defer resp.Body.Close()

	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// handleDirectConnect 处理直连的CONNECT请求
func (p *HTTPOverSSH) handleDirectConnect(w http.ResponseWriter, req *http.Request) {
	destConn, err := net.DialTimeout("tcp", req.Host, 10*time.Second)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	defer destConn.Close()
	if !p.connections.add(destConn) {
		return
	}
	defer p.connections.remove(destConn)

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		destConn.Close()
		http.Error(w, "Hijacking not supported", http.StatusInternalServerError)
		return
	}
	clientConn, buffered, err := hijacker.Hijack()
	if err != nil {
		destConn.Close()
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	defer clientConn.Close()
	if !p.connections.add(clientConn) {
		return
	}
	defer p.connections.remove(clientConn)

	_, err = clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
	if err != nil {
		clientConn.Close()
		destConn.Close()
		return
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go transfer(&wg, destConn, struct {
		io.Reader
		io.Closer
	}{buffered.Reader, clientConn}, "client->direct", p.logger)
	go transfer(&wg, clientConn, destConn, "direct->client", p.logger)
	wg.Wait()
}

// transfer 封装了双向数据转发
func transfer(wg *sync.WaitGroup, destination io.WriteCloser, source io.ReadCloser, direction string, log *logger.Logger) {
	defer wg.Done()
	n, err := io.Copy(destination, source)
	if err == nil {
		// EOF ends only this direction; the peer may still have a response to send.
		if half, ok := destination.(interface{ CloseWrite() error }); ok {
			err = half.CloseWrite()
		} else {
			err = destination.Close()
		}
	}
	if err != nil {
		destination.Close()
		source.Close()
	}
	if err != nil && !isConnectionClosed(err) {
		log.Debugf("%s 传输错误: %v", direction, err)
	}
	log.Debugf("%s 传输完成: %d 字节", direction, n)
}

// Both accepted clients and outbound connections must be closed on shutdown.
type connectionSet struct {
	mu          sync.Mutex
	closed      bool
	connections map[net.Conn]struct{}
}

func (s *connectionSet) add(conn net.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		conn.Close()
		return false
	}
	if s.connections == nil {
		s.connections = make(map[net.Conn]struct{})
	}
	s.connections[conn] = struct{}{}
	return true
}

func (s *connectionSet) remove(conn net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.connections, conn)
}

func (s *connectionSet) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	for conn := range s.connections {
		conn.Close()
	}
	s.connections = nil
}

// isConnectionClosed 判断是否是常见的连接关闭错误
func isConnectionClosed(err error) bool {
	if err == nil {
		return false
	}
	return err == io.EOF ||
		strings.Contains(err.Error(), "connection reset by peer") ||
		strings.Contains(err.Error(), "broken pipe") ||
		strings.Contains(err.Error(), "use of closed network connection")
}
