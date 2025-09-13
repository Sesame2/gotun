package proxy

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Sesame2/gotun/internal/config"
	"github.com/Sesame2/gotun/internal/logger"
)

// HTTPOverSSH 表示基于SSH的HTTP代理
type HTTPOverSSH struct {
	cfg    *config.Config
	ssh    *SSHClient
	logger *logger.Logger
	server *http.Server
}

// NewHTTPOverSSH 创建HTTP代理
func NewHTTPOverSSH(cfg *config.Config, log *logger.Logger) (*HTTPOverSSH, error) {
	log.Info("初始化HTTP-over-SSH代理")

	sshClient, err := NewSSHClient(cfg, log)
	if err != nil {
		return nil, err
	}

	proxy := &HTTPOverSSH{
		cfg:    cfg,
		ssh:    sshClient,
		logger: log,
	}

	return proxy, nil
}

// Start 启动HTTP代理服务
func (p *HTTPOverSSH) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", p.handleHTTP)

	p.server = &http.Server{
		Addr:    p.cfg.ListenAddr,
		Handler: mux,
	}

	p.logger.Infof("HTTP代理服务器启动在 %s", p.cfg.ListenAddr)
	return p.server.ListenAndServe()
}

// handleHTTP 处理HTTP请求
func (p *HTTPOverSSH) handleHTTP(w http.ResponseWriter, req *http.Request) {
	startTime := time.Now()
	clientIP := req.RemoteAddr

	if !req.URL.IsAbs() {
		p.logger.Warnf("收到非绝对URL请求: %s", req.URL.String())
		http.Error(w, "需要绝对URL", http.StatusBadRequest)
		return
	}

	p.logger.Infof("来自 %s 的请求: %s %s", clientIP, req.Method, req.URL.String())

	targetAddr := req.URL.Host
	if p.cfg.SSHTargetDial != "" {
		p.logger.Debugf("使用指定的目标地址覆盖: %s", p.cfg.SSHTargetDial)
		targetAddr = p.cfg.SSHTargetDial
	}

	// 添加默认端口
	if !strings.Contains(targetAddr, ":") {
		if req.URL.Scheme == "https" {
			targetAddr = targetAddr + ":443"
			p.logger.Debugf("添加HTTPS默认端口, 目标地址变为: %s", targetAddr)
		} else {
			targetAddr = targetAddr + ":80"
			p.logger.Debugf("添加HTTP默认端口, 目标地址变为: %s", targetAddr)
		}
	}

	p.logger.Debugf("通过SSH连接到目标地址: %s", targetAddr)
	conn, err := p.ssh.Dial("tcp", targetAddr)
	if err != nil {
		p.logger.Errorf("无法通过SSH连接到目标 %s: %v", targetAddr, err)
		http.Error(w, "无法通过SSH连接到目标", http.StatusBadGateway)
		return
	}
	defer conn.Close()

	// 发送HTTP请求
	p.logger.Debugf("向目标发送HTTP请求: %s %s", req.Method, req.URL.String())
	err = req.Write(conn)
	if err != nil {
		p.logger.Errorf("写入请求到远程服务器失败: %v", err)
		http.Error(w, "写入请求到远程服务器失败", http.StatusInternalServerError)
		return
	}

	// 读取HTTP响应
	p.logger.Debug("从目标服务器读取HTTP响应")
	resp, err := http.ReadResponse(bufio.NewReader(conn), req)
	if err != nil {
		p.logger.Errorf("从远程服务器读取响应失败: %v", err)
		http.Error(w, "从远程服务器读取响应失败", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// 复制响应头
	p.logger.Debug("复制响应头到客户端")
	for k, v := range resp.Header {
		for _, vv := range v {
			w.Header().Add(k, vv)
		}
	}

	// 发送响应
	w.WriteHeader(resp.StatusCode)
	written, err := io.Copy(w, resp.Body)
	if err != nil {
		p.logger.Errorf("写入响应到客户端失败: %v", err)
		return
	}

	duration := time.Since(startTime)
	p.logger.Infof("请求完成: %s %s - %d (%d bytes) - %v", req.Method, req.URL.String(), resp.StatusCode, written, duration)
}

// Close 关闭代理服务
func (p *HTTPOverSSH) Close() error {
	p.logger.Info("正在关闭代理服务")

	var err error
	if p.server != nil {
		p.logger.Debug("关闭HTTP服务器")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err = p.server.Shutdown(ctx)
		if err != nil {
			p.logger.Errorf("HTTP服务器关闭失败: %v", err)
		}
	}

	if p.ssh != nil {
		sshErr := p.ssh.Close()
		if sshErr != nil && err == nil {
			err = sshErr
		}
	}

	return err
}
