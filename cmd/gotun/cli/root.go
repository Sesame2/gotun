package cli

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Sesame2/gotun/internal/config"
	"github.com/Sesame2/gotun/internal/logger"
	"github.com/Sesame2/gotun/internal/proxy"
	"github.com/Sesame2/gotun/internal/sysproxy"
	"github.com/spf13/cobra"
)

var (
	Version = "dev"
	cfg     = config.NewConfig()
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

		httpProxy, err := proxy.NewHTTPOverSSH(cfg, log)
		if err != nil {
			return fmt.Errorf("代理初始化失败: %w", err)
		}

		var proxyMgr *sysproxy.Manager
		if cfg.SystemProxy {
			proxyMgr = sysproxy.NewManager(log, cfg.ListenAddr)
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
				log.Errorf("代理服务启动失败: %v", err)
				sigChan <- syscall.SIGTERM
			}
		}()

		fmt.Println("\n代理服务已启动:", "http://"+cfg.ListenAddr)
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
			log.Errorf("关闭代理服务失败: %v", err)
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
