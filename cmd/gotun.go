package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/Sesame2/gotun/internal/config"
	"github.com/Sesame2/gotun/internal/logger"
	"github.com/Sesame2/gotun/internal/proxy"
)

var (
	Version = "dev" // 由构建时的ldflags填充
)

func main() {
	cfg := config.NewConfig()
	cfg.ParseFlags()

	// 初始化日志系统
	log := logger.NewLogger(cfg.Verbose)
	log.Infof("GoTun %s 启动中...", Version)

	if err := cfg.Validate(); err != nil {
		log.Errorf("配置验证失败: %v", err)
		os.Exit(1)
	}

	// 创建并启动代理
	proxy, err := proxy.NewHTTPOverSSH(cfg, log)
	if err != nil {
		log.Errorf("创建代理失败: %v", err)
		os.Exit(1)
	}

	// 优雅退出处理
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		log.Infof("收到信号 %v, 正在关闭代理服务...", sig)
		if err := proxy.Close(); err != nil {
			log.Errorf("关闭代理时出错: %v", err)
		}
		os.Exit(0)
	}()

	// 启动代理服务
	log.Infof("HTTP代理正在监听: %s", cfg.ListenAddr)
	if err := proxy.Start(); err != nil {
		log.Errorf("代理服务启动失败: %v", err)
		os.Exit(1)
	}
}
