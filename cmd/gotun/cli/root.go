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
		// 自动开启 TUN 模式: 如果指定了 Global, Route 或 NAT
		if cfg.TunGlobal || len(cfg.TunRoute) > 0 || len(aliasFlags) > 0 {
			cfg.TunMode = true
		}

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
				return fmt.Errorf("无效的别名格式: %s, 应为 Src:Dst (例如 10.0.0.1:192.168.1.1 或 10.0.0.0/24:192.168.1.0/24)", alias)
			}

			// Helper: parse IP or CIDR to *net.IPNet
			parseNet := func(s string) (*net.IPNet, error) {
				// 尝试解析为 CIDR
				_, ipNet, err := net.ParseCIDR(s)
				if err == nil {
					return ipNet, nil
				}
				// 尝试解析为单 IP
				ip := net.ParseIP(s)
				if ip == nil {
					return nil, fmt.Errorf("无效的 IP 或网段: %s", s)
				}
				// 转换为 /32 (IPv4)
				ip = ip.To4()
				if ip == nil {
					return nil, fmt.Errorf("不支持 IPv6: %s", s)
				}
				return &net.IPNet{IP: ip, Mask: net.CIDRMask(32, 32)}, nil
			}

			srcNet, err := parseNet(parts[0])
			if err != nil {
				return err
			}
			dstNet, err := parseNet(parts[1])
			if err != nil {
				return err
			}

			// 校验掩码大小是否一致
			srcSize, _ := srcNet.Mask.Size()
			dstSize, _ := dstNet.Mask.Size()
			if srcSize != dstSize {
				return fmt.Errorf("源网段和目标网段掩码长度不一致: %s (%d) != %s (%d)", parts[0], srcSize, parts[1], dstSize)
			}

			cfg.SubnetAliases = append(cfg.SubnetAliases, config.SubnetAlias{
				Src: srcNet,
				Dst: dstNet,
			})
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
			fmt.Printf("TUN Mode: Enabled (CIDR: %s)\n", cfg.TunCIDR)
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
	// --- Group 1: SSH Connection ---
	rootCmd.PersistentFlags().StringVarP(&cfg.SSHPort, "port", "p", "22", "SSH服务器端口")
	rootCmd.PersistentFlags().StringVar(&cfg.SSHPassword, "pass", "", "SSH密码 (不安全, 建议使用交互式认证)")
	rootCmd.PersistentFlags().StringVarP(&cfg.SSHKeyFile, "identity_file", "i", "", "用于认证的私钥文件路径")
	rootCmd.PersistentFlags().StringSliceVarP(&cfg.JumpHosts, "jump", "J", []string{}, "跳板机列表,用逗号分隔 (格式: user@host:port)")
	rootCmd.PersistentFlags().DurationVar(&cfg.Timeout, "timeout", 10*time.Second, "连接超时时间")

	// --- Group 2: Proxy Services ---
	rootCmd.PersistentFlags().StringVarP(&cfg.ListenAddr, "listen", "l", ":8080", "本地HTTP代理监听地址 [已废弃，推荐使用 --http]")
	rootCmd.PersistentFlags().StringVar(&cfg.ListenAddr, "http", ":8080", "本地HTTP代理监听地址 (别名: --listen)")
	rootCmd.PersistentFlags().StringVar(&cfg.SocksAddr, "socks5", "", "SOCKS5 代理监听地址 (例如 :1080)")
	rootCmd.PersistentFlags().BoolVar(&cfg.SystemProxy, "sys-proxy", true, "自动设置/恢复系统代理")
	rootCmd.PersistentFlags().StringVar(&cfg.HTTPUpstream, "http-upstream", "", "强制将所有HTTP请求转发到此上游 (格式: host:port)")
	// 兼容旧参数 target (隐藏)
	rootCmd.PersistentFlags().StringVar(&cfg.HTTPUpstream, "target", "", "DEPRECATED: use --http-upstream")
	rootCmd.PersistentFlags().MarkHidden("target")

	// --- Group 3: TUN Mode ---
	rootCmd.PersistentFlags().BoolVar(&cfg.TunMode, "tun", false, "启用 TUN 模式 (VPN 模式)")
	rootCmd.PersistentFlags().BoolVarP(&cfg.TunGlobal, "tun-global", "g", false, "启用全局 TUN 模式 (转发所有流量)")
	rootCmd.PersistentFlags().StringVar(&cfg.TunCIDR, "tun-ip", "10.0.0.1/24", "TUN 设备 CIDR 地址")
	rootCmd.PersistentFlags().StringSliceVar(&cfg.TunRoute, "tun-route", []string{}, "添加静态路由到 TUN (CIDR格式, 可多次使用)")
	rootCmd.PersistentFlags().StringSliceVar(&aliasFlags, "tun-nat", []string{}, "NAT 映射规则 (格式: SrcCIDR:DstCIDR)")

	// --- Group 4: General ---
	rootCmd.PersistentFlags().BoolVarP(&cfg.Verbose, "verbose", "v", false, "启用详细日志")
	rootCmd.PersistentFlags().StringVar(&cfg.LogFile, "log", "", "日志文件路径")
	rootCmd.PersistentFlags().StringVar(&cfg.RuleFile, "rules", "", "代理规则配置文件路径")
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
