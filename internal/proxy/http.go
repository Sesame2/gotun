package proxy

import (
	"bufio"
	"context"
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
	cfg    *config.Config
	ssh    *SSHClient
	logger *logger.Logger
	server *http.Server
	router *router.Router
}

// NewHTTPOverSSH 创建HTTP代理
func NewHTTPOverSSH(cfg *config.Config, log *logger.Logger) (*HTTPOverSSH, error) {
	log.Info("初始化HTTP-over-SSH代理")

	sshClient, err := NewSSHClient(cfg, log)
	if err != nil {
		return nil, err
	}

	var r *router.Router
	if cfg.RuleFile != "" {
		r, err = router.NewRouter(cfg.RuleFile)
		if err != nil {
			log.Warnf("加载规则文件失败: %v。将以全局代理模式运行。", err)
		} else {
			log.Infof("已加载规则文件: %s", cfg.RuleFile)
		}
	}

	proxy := &HTTPOverSSH{
		cfg:    cfg,
		ssh:    sshClient,
		logger: log,
		router: r,
	}

	return proxy, nil
}

// Start 启动HTTP代理服务
func (p *HTTPOverSSH) Start() error {
	p.server = &http.Server{
		Addr:    p.cfg.ListenAddr,
		Handler: http.HandlerFunc(p.handleHTTP),
	}

	p.logger.Infof("HTTP/HTTPS 代理服务器启动在 %s", p.cfg.ListenAddr)
	err := p.server.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

// handleHTTP 处理所有传入请求的入口
func (p *HTTPOverSSH) handleHTTP(w http.ResponseWriter, req *http.Request) {
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
	if p.cfg.SSHTargetDial != "" {
		p.logger.Debugf("使用指定的目标地址覆盖: %s", p.cfg.SSHTargetDial)
		targetAddr = p.cfg.SSHTargetDial
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

// handleHTTPSConnect 处理CONNECT请求
func (p *HTTPOverSSH) handleHTTPSConnect(w http.ResponseWriter, req *http.Request) {
	startTime := time.Now()

	// 路由判断
	if p.router != nil {
		action := p.router.Match(req.Host)
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

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		p.logger.Error("代理服务器不支持连接劫持")
		http.Error(w, "代理服务器不支持HTTPS连接", http.StatusInternalServerError)
		return
	}

	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		p.logger.Errorf("连接劫持失败: %v", err)
		http.Error(w, "连接劫持失败", http.StatusServiceUnavailable)
		return
	}

	_, err = clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
	if err != nil {
		p.logger.Errorf("向客户端写入隧道建立响应失败: %v", err)
		clientConn.Close()
		sshConn.Close()
		return
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go transfer(&wg, sshConn, clientConn, "client->ssh", p.logger)
	go transfer(&wg, clientConn, sshConn, "ssh->client", p.logger)
	wg.Wait()

	duration := time.Since(startTime)
	p.logger.Infof("HTTPS隧道已关闭: %s, 持续时间: %v", req.Host, duration)
}

// Close 关闭代理服务
func (p *HTTPOverSSH) Close() error {
	p.logger.Info("正在关闭代理服务")
	var err error
	if p.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err = p.server.Shutdown(ctx)
	}
	if p.ssh != nil {
		sshErr := p.ssh.Close()
		if sshErr != nil && err == nil {
			err = sshErr
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

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "Hijacking not supported", http.StatusInternalServerError)
		return
	}
	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	_, err = clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
	if err != nil {
		clientConn.Close()
		destConn.Close()
		return
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go transfer(&wg, destConn, clientConn, "client->direct", p.logger)
	go transfer(&wg, clientConn, destConn, "direct->client", p.logger)
	wg.Wait()
}

// transfer 封装了双向数据转发
func transfer(wg *sync.WaitGroup, destination io.WriteCloser, source io.ReadCloser, direction string, log *logger.Logger) {
	defer wg.Done()
	defer destination.Close()
	defer source.Close()
	n, err := io.Copy(destination, source)
	if err != nil && !isConnectionClosed(err) {
		log.Debugf("%s 传输错误: %v", direction, err)
	}
	log.Debugf("%s 传输完成: %d 字节", direction, n)
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
