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
	"github.com/Sesame2/gotun/internal/sysproxy"
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
	fmt.Println("  gotun user@example.com                           # 使用默认端口22")
	fmt.Println("  gotun -p 2222 user@example.com                   # 指定SSH端口")
	fmt.Println("  gotun -i ~/.ssh/id_rsa user@example.com          # 使用私钥认证")
	fmt.Println("  gotun -listen :8888 user@example.com             # 自定义代理监听端口")
	fmt.Println("  gotun -sys-proxy user@example.com                # 自动设置系统代理")
	fmt.Println("  gotun -J jump@proxy.com user@target.com          # 使用单个跳板机")
	fmt.Println("  gotun -J jump1@proxy1.com,jump2@proxy2.com user@target.com  # 多跳板机")
}

func main() {
	// 自定义帮助信息
	flag.Usage = printUsage

	// 初始化配置
	cfg := config.NewConfig()
	cfg.ParseFlags()

	// 显示帮助
	if len(os.Args) == 1 || (len(os.Args) == 2 && (os.Args[1] == "-h" || os.Args[1] == "--help")) {
		flag.Usage()
		os.Exit(0)
	}

	// 验证配置
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "配置错误: %v\n", err)
		os.Exit(1)
	}

	// 初始化日志系统
	log := logger.NewLogger(cfg.Verbose)
	if cfg.LogFile != "" {
		if err := log.SetLogFile(cfg.LogFile); err != nil {
			fmt.Fprintf(os.Stderr, "设置日志文件失败: %v\n", err)
			os.Exit(1)
		}
		log.Infof("日志将输出到文件: %s", cfg.LogFile)
	}
	log.Infof("GoTun %s 启动中...", Version)

	// 创建并启动代理
	httpProxy, err := proxy.NewHTTPOverSSH(cfg, log)
	if err != nil {
		log.Errorf("代理初始化失败: %v", err)
		os.Exit(1)
	}

	// 创建系统代理管理器
	var proxyMgr *sysproxy.Manager
	if cfg.SystemProxy {
		proxyMgr = sysproxy.NewManager(log, cfg.ListenAddr)
	}

	// 优雅退出处理
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// 启动代理服务
	go func() {
		// 如果启用了系统代理，设置它
		if cfg.SystemProxy && proxyMgr != nil {
			if err := proxyMgr.Enable(); err != nil {
				log.Errorf("设置系统代理失败: %v", err)
			} else {
				log.Info("系统代理设置成功，已清除所有代理例外规则")
			}
		}

		// 启动HTTP代理
		err := httpProxy.Start()
		if err != nil {
			log.Errorf("代理服务启动失败: %v", err)
			sigChan <- syscall.SIGTERM // 触发关闭
		}
	}()

	// 显示连接信息
	fmt.Println("\n代理服务已启动:", "http://"+cfg.ListenAddr)
	if len(cfg.JumpHosts) > 0 {
		fmt.Printf("跳板机链: %s -> %s\n", fmt.Sprintf("%v", cfg.JumpHosts), cfg.SSHServer)
	} else {
		fmt.Println("直连SSH服务器:", cfg.SSHServer)
	}
	if cfg.SystemProxy {
		fmt.Println("系统代理已启用，所有流量将通过代理")
	}
	fmt.Println("按 Ctrl+C 退出")

	// 等待退出信号
	<-sigChan
	log.Info("收到信号, 正在关闭代理服务...")

	// 清理系统代理设置
	if cfg.SystemProxy && proxyMgr != nil {
		if err := proxyMgr.Disable(); err != nil {
			log.Errorf("恢复系统代理设置失败: %v", err)
		} else {
			log.Info("系统代理设置已恢复")
		}
	}

	// 关闭代理
	if err := httpProxy.Close(); err != nil {
		log.Errorf("关闭代理服务失败: %v", err)
	}
}
