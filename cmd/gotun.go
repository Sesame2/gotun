package main

import (
	"flag"
	"fmt"
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

// printUsage 打印自定义使用帮助
func printUsage() {
	fmt.Printf("GoTun %s - 基于SSH的轻量级HTTP代理工具\n\n", Version)
	fmt.Println("用法:")
	fmt.Println("  gotun [选项] [user@host]")
	fmt.Println("\n选项:")
	flag.PrintDefaults()
	fmt.Println("\n示例:")
	fmt.Println("  gotun user@example.com                 # 使用默认端口22")
	fmt.Println("  gotun -p 2222 user@example.com         # 指定SSH端口")
	fmt.Println("  gotun -i ~/.ssh/id_rsa user@example.com # 使用私钥认证")
	fmt.Println("  gotun -listen :8888 user@example.com   # 自定义代理监听端口")
}

func main() {
	// 自定义帮助信息
	flag.Usage = printUsage

	// 初始化配置
	cfg := config.NewConfig()
	cfg.ParseFlags()

	// 显示帮助
	if len(os.Args) == 1 || (len(os.Args) == 2 && (os.Args[1] == "-h" || os.Args[1] == "--help")) {
		printUsage()
		return
	}

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
	fmt.Printf("\n代理服务已启动: http://%s\n", cfg.ListenAddr)
	fmt.Printf("使用 %s@%s 作为SSH中继服务器\n", cfg.SSHUser, cfg.SSHServer)
	fmt.Println("按 Ctrl+C 退出")

	if err := proxy.Start(); err != nil {
		log.Errorf("代理服务启动失败: %v", err)
		os.Exit(1)
	}
}
