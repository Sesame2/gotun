// gotun-gui is a web-based graphical interface for the gotun SSH proxy tool.
//
// It starts a local web server on 127.0.0.1:8089 (by default) and opens the
// control panel in the browser, allowing users to configure and manage the
// gotun SSH tunnel without using the command line.
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/Sesame2/gotun/gui"
	"github.com/Sesame2/gotun/internal/config"
	"github.com/spf13/cobra"
)

var (
	Version = "dev"

	cfg     = config.NewConfig()
	guiAddr string
)

var rootCmd = &cobra.Command{
	Use:     "gotun-gui",
	Version: Version,
	Short:   "GoTun 图形界面管理工具",
	Long: `gotun-gui 启动一个本地 Web 控制面板，让你通过浏览器管理 gotun 代理。

无需参数即可启动控制面板，在启动后可通过界面配置 SSH 服务器信息并控制代理的启停。`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// If a SSH target is provided (user@host), pre-fill config.
		if len(args) == 1 {
			parts := strings.SplitN(args[0], "@", 2)
			if len(parts) == 2 {
				cfg.SSHUser = parts[0]
				host := parts[1]
				if !strings.Contains(host, ":") {
					host = fmt.Sprintf("%s:%s", host, cfg.SSHPort)
				}
				cfg.SSHServer = host
			}
		}

		app := gui.NewApp(cfg, guiAddr)
		fmt.Printf("GoTun GUI 正在启动，请在浏览器中打开 http://%s\n", guiAddr)
		return app.Run()
	},
}

func init() {
	rootCmd.Flags().StringVar(&guiAddr, "gui-addr", "127.0.0.1:8089", "Web 控制面板监听地址")
	rootCmd.Flags().StringVarP(&cfg.SSHPort, "port", "p", "22", "SSH 服务器端口")
	rootCmd.Flags().StringVar(&cfg.SSHPassword, "pass", "", "SSH 密码")
	rootCmd.Flags().StringVarP(&cfg.SSHKeyFile, "identity_file", "i", "", "SSH 私钥文件路径")
	rootCmd.Flags().StringVarP(&cfg.ListenAddr, "http", "l", ":8080", "HTTP 代理监听地址")
	rootCmd.Flags().StringVar(&cfg.SocksAddr, "socks5", ":1080", "SOCKS5 代理监听地址")
	rootCmd.Flags().BoolVar(&cfg.SystemProxy, "sys-proxy", true, "自动设置/恢复系统代理")
	rootCmd.Flags().BoolVarP(&cfg.Verbose, "verbose", "v", false, "启用详细日志")
	rootCmd.Flags().StringVar(&cfg.RuleFile, "rules", "", "代理规则配置文件路径")
	rootCmd.Args = cobra.MaximumNArgs(1)
}

func main() {
	rootCmd.Version = Version
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
