package cli

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/Sesame2/gotun/internal/config"
	"github.com/Sesame2/gotun/internal/logger"
	"github.com/Sesame2/gotun/internal/proxy"
	"github.com/Sesame2/gotun/internal/router"
	"github.com/Sesame2/gotun/internal/sysproxy"
	"github.com/Sesame2/gotun/internal/tun"
	"github.com/spf13/cobra"
)

var (
	Version    = "dev"
	cfg        = config.NewConfig()
	aliasFlags []string
)

// rootCmd 代表不带任何子命令时的基础命令
var rootCmd = &cobra.Command{
	Use:     "gotun [user@host]",
	Version: Version,
	Short:   "基于SSH的轻量级HTTP代理工具",
	Long: `gotun 是一个通过SSH隧道实现HTTP代理的命令行工具。
它可以帮助您安全地访问内网资源或将远程主机作为网络出口。`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// 检查是否启用 TUN 模式且非 root 用户 (Windows 除外)
		if cfg.TunMode && runtime.GOOS != "windows" && os.Geteuid() != 0 {
			fmt.Println("TUN 模式需要 root 权限，尝试使用 sudo 重新启动...")

			exe, err := os.Executable()
			if err != nil {
				return fmt.Errorf("无法获取当前可执行文件路径: %w", err)
			}

			// 构建 sudo 命令
			sudoArgs := []string{"sudo", exe}
			sudoArgs = append(sudoArgs, os.Args[1:]...)

			// 执行 sudo
			syscall.Exec("/usr/bin/sudo", sudoArgs, os.Environ())
			return nil
		}

		// 从参数填充SSH用户和服务器
		user, host, err := parseSSHTarget(args[0])
		if err != nil {
			return err
		}
		cfg.SSHUser = user
		cfg.SSHServer = host

		// 组合服务器地址和端口
		if cfg.SSHServer != "" && !strings.Contains(cfg.SSHServer, ":") {
			cfg.SSHServer = fmt.Sprintf("%s:%s", cfg.SSHServer, cfg.SSHPort)
		}

		// 解析 alias 参数到 Config
		for _, alias := range aliasFlags {
			parts := strings.Split(alias, ":")
			if len(parts) != 2 {
				return fmt.Errorf("无效的别名格式: %s, 应为 虚拟IP:目标地址", alias)
			}
			// 简单的 IP 校验
			if net.ParseIP(parts[0]) == nil {
				return fmt.Errorf("无效的虚拟IP: %s", parts[0])
			}
			cfg.IPAliases[parts[0]] = parts[1]
		}

		// 验证配置
		if err := cfg.Validate(); err != nil {
			return fmt.Errorf("配置错误: %w", err)
		}

		log := logger.NewLogger(cfg.Verbose)
		if cfg.LogFile != "" {
			if err := log.SetLogFile(cfg.LogFile); err != nil {
				return fmt.Errorf("设置日志文件失败: %w", err)
			}
			log.Infof("日志将输出到文件: %s", cfg.LogFile)
		}
		log.Infof("GoTun %s 启动中...", Version)

		// 1. 初始化 Router
		var r *router.Router
		if cfg.RuleFile != "" {
			var err error
			r, err = router.NewRouter(cfg.RuleFile)
			if err != nil {
				log.Warnf("加载规则文件失败: %v。将以全局代理模式运行。", err)
			} else {
				log.Infof("已加载规则文件: %s", cfg.RuleFile)
			}
		}

		// 2. 初始化 SSHClient
		sshClient, err := proxy.NewSSHClient(cfg, log)
		if err != nil {
			return fmt.Errorf("SSH连接失败: %w", err)
		}
		defer sshClient.Close()

		// 3. 初始化 HTTP 代理
		httpProxy, err := proxy.NewHTTPOverSSH(cfg, log, sshClient, r)
		if err != nil {
			return fmt.Errorf("HTTP代理初始化失败: %w", err)
		}

		// 4. 初始化 SOCKS5 代理
		var socksProxy *proxy.SOCKS5OverSSH
		if cfg.SocksAddr != "" {
			socksProxy, err = proxy.NewSOCKS5OverSSH(cfg, log, sshClient, r)
			if err != nil {
				return fmt.Errorf("SOCKS5代理初始化失败: %w", err)
			}
		}

		var proxyMgr *sysproxy.Manager
		if cfg.SystemProxy {
			proxyMgr = sysproxy.NewManager(log, cfg.ListenAddr, cfg.SocksAddr)
		}

		// 5. 初始化 TUN 模式
		var tunService *tun.TunService
		if cfg.TunMode {
			tunService, err = tun.NewTunService(cfg, log, sshClient)
			if err != nil {
				return fmt.Errorf("TUN服务初始化失败: %w", err)
			}
		}

		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

		go func() {
			if cfg.SystemProxy && proxyMgr != nil {
				if err := proxyMgr.Enable(); err != nil {
					log.Errorf("设置系统代理失败: %v", err)
				}
			}
			if err := httpProxy.Start(); err != nil {
				log.Errorf("HTTP代理服务启动失败: %v", err)
				sigChan <- syscall.SIGTERM
			}
		}()

		if tunService != nil {
			go func() {
				if err := tunService.Start(); err != nil {
					log.Errorf("TUN服务启动失败: %v", err)
					sigChan <- syscall.SIGTERM
				}
			}()
		}

		if socksProxy != nil {
			go func() {
				if err := socksProxy.Start(); err != nil {
					log.Errorf("SOCKS5代理服务启动失败: %v", err)
					sigChan <- syscall.SIGTERM
				}
			}()
		}

		fmt.Println("\n代理服务已启动:")
		fmt.Println("HTTP Proxy:", "http://"+cfg.ListenAddr)
		if cfg.SocksAddr != "" {
			fmt.Println("SOCKS5 Proxy:", "socks5://"+cfg.SocksAddr)
		}
		if cfg.TunMode {
			fmt.Printf("TUN Mode: Enabled (IP: %s, Mask: %s)\n", cfg.TunAddr, cfg.TunMask)
		}

		if len(cfg.JumpHosts) > 0 {
			fmt.Printf("跳板机链: %s -> %s\n", fmt.Sprintf("%v", cfg.JumpHosts), cfg.SSHServer)
		} else {
			fmt.Println("直连SSH服务器:", cfg.SSHServer)
		}
		if cfg.SystemProxy {
			fmt.Println("系统代理已启用")
		}
		if cfg.RuleFile != "" {
			fmt.Println("自定义路由规则已启用:", cfg.RuleFile)
		}
		fmt.Println("按 Ctrl+C 退出")

		<-sigChan
		log.Info("收到信号, 正在关闭代理服务...")

		if cfg.SystemProxy && proxyMgr != nil {
			if err := proxyMgr.Disable(); err != nil {
				log.Errorf("恢复系统代理设置失败: %v", err)
			}
		}

		if err := httpProxy.Close(); err != nil {
			log.Errorf("关闭HTTP代理服务失败: %v", err)
		}

		// 新增关闭逻辑
		if socksProxy != nil {
			if err := socksProxy.Close(); err != nil {
				log.Errorf("关闭SOCKS5代理服务失败: %v", err)
			}
		}

		if tunService != nil {
			if err := tunService.Close(); err != nil {
				log.Errorf("关闭TUN服务失败: %v", err)
			}
		}

		return nil
	},
}

// init函数在main函数之前执行，定义原来所有的flag
func init() {
	// 使用 PersistentFlags，这样未来如果添加子命令，它们也能继承这些flag
	rootCmd.PersistentFlags().StringVarP(&cfg.ListenAddr, "listen", "l", ":8080", "本地HTTP代理监听地址")
	rootCmd.PersistentFlags().StringVarP(&cfg.SSHPort, "port", "p", "22", "SSH服务器端口")
	rootCmd.PersistentFlags().StringVar(&cfg.SSHPassword, "pass", "", "SSH密码 (不安全, 建议使用交互式认证)")
	rootCmd.PersistentFlags().StringVarP(&cfg.SSHKeyFile, "identity_file", "i", "", "用于认证的私钥文件路径")
	rootCmd.PersistentFlags().StringSliceVarP(&cfg.JumpHosts, "jump", "J", []string{}, "跳板机列表,用逗号分隔 (格式: user@host:port)")
	rootCmd.PersistentFlags().StringVar(&cfg.SSHTargetDial, "target", "", "可选的目标网络覆盖")
	rootCmd.PersistentFlags().DurationVar(&cfg.Timeout, "timeout", 10*time.Second, "连接超时时间")
	rootCmd.PersistentFlags().BoolVarP(&cfg.Verbose, "verbose", "v", false, "启用详细日志")
	rootCmd.PersistentFlags().StringVar(&cfg.LogFile, "log", "", "日志文件路径")
	rootCmd.PersistentFlags().BoolVar(&cfg.SystemProxy, "sys-proxy", true, "自动设置/恢复系统代理")
	rootCmd.PersistentFlags().StringVar(&cfg.RuleFile, "rules", "", "代理规则配置文件路径")
	rootCmd.PersistentFlags().StringVar(&cfg.SocksAddr, "socks5", "", "SOCKS5 代理监听地址")
	rootCmd.PersistentFlags().BoolVar(&cfg.TunMode, "tun", false, "启用 TUN 模式 (TCP over SSH)")
	rootCmd.PersistentFlags().StringVar(&cfg.TunAddr, "tun-addr", "10.0.0.1", "TUN 设备 IP 地址")
	rootCmd.PersistentFlags().StringVar(&cfg.TunMask, "tun-mask", "255.255.255.0", "TUN 设备子网掩码")
	rootCmd.PersistentFlags().StringSliceVar(&cfg.TunRoutes, "tun-routes", []string{}, "需要路由到 TUN 的网段 (CIDR), 用逗号分隔")
	rootCmd.PersistentFlags().BoolVarP(&cfg.TunGlobal, "global", "g", false, "启用全局 TUN 模式 (转发所有流量)")
	rootCmd.PersistentFlags().StringSliceVar(&aliasFlags, "alias", []string{}, "IP别名映射，格式: 虚拟IP:目标地址 (例如 10.10.10.10:127.0.0.1)")
}

func Execute(version string) {
	rootCmd.Version = version
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// 解析SSH目标格式(user@host)
func parseSSHTarget(target string) (string, string, error) {
	if target == "" {
		return "", "", nil
	}

	parts := strings.Split(target, "@")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("无效的SSH目标格式，需要 user@host 格式")
	}

	user := parts[0]
	host := parts[1]

	// 检查是否有效
	if user == "" || host == "" {
		return "", "", fmt.Errorf("用户名或主机名不能为空")
	}

	return user, host, nil
}
