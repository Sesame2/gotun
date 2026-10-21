package main

import (
	"context"
	"fmt"
	"github.com/Sesame2/gotun/gui/internal/tray"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Sesame2/gotun/internal/service"
	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// LogEntry 日志条目
type LogEntry struct {
	ID        int64  `json:"id"`
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Message   string `json:"message"`
}

// App struct
type App struct {
	actionMu      sync.Mutex
	actionBusy    atomic.Bool
	stateMu       sync.RWMutex
	httpProfileID string
	tunProfileID  string
	quitting      atomic.Bool
	trayReady     atomic.Bool
	trayMu        sync.Mutex
	tray          tray.Native
	trayDone      chan struct{}
	trayUpdates   chan struct{}
	trayCloseOnce sync.Once
	trayInit      sync.Once
	ctx           context.Context
	proxyService  *service.ProxyService
	tunService    *service.TunService
	configService *service.ConfigService

	// 日志系统
	logMu      sync.RWMutex
	logs       []LogEntry
	logCounter int64
	maxLogs    int
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{
		trayDone: make(chan struct{}), trayUpdates: make(chan struct{}, 1),
		proxyService: service.NewProxyService(),
		tunService:   service.NewTunService(),
		logs:         make([]LogEntry, 0, 1000),
		maxLogs:      1000,
	}
}

// startup is called when the app starts
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	// 初始化配置服务
	configSvc, err := service.NewConfigService()
	if err != nil {
		a.AddLog("ERROR", fmt.Sprintf("初始化配置服务失败: %v", err))
	} else {
		a.configService = configSvc
		a.AddLog("INFO", "配置服务初始化成功")
	}

	// 设置代理服务的日志回调
	a.proxyService.SetLogCallback(func(level, message string) {
		a.AddLog(level, message)
	})

	// 设置TUN服务的日志回调
	a.tunService.SetLogCallback(func(level, message string) {
		a.AddLog(level, message)
	})

	// 应用配置
	if a.configService != nil {
		settings := a.configService.GetSettings()
		a.proxyService.SetVerbose(settings.Verbose)
		if settings.LogFile != "" {
			if err := a.proxyService.SetLogFile(settings.LogFile); err != nil {
				a.AddLog("ERROR", fmt.Sprintf("设置日志文件失败: %v", err))
			}
		}
	}

	a.AddLog("INFO", "GoTun GUI 启动完成")
}

// shutdown is called when the app is closing
func (a *App) shutdown(ctx context.Context) {
	a.quitting.Store(true)
	a.trayCloseOnce.Do(func() { close(a.trayDone) })
	a.trayMu.Lock()
	if a.tray != nil {
		a.tray.Close()
	}
	a.trayReady.Store(false)
	a.trayMu.Unlock()
	a.beginAction()
	defer a.endAction()
	if err := a.stopAllProxiesLocked(); err != nil {
		a.AddLog("ERROR", err.Error())
	}
}

// ==================== 代理服务相关方法 ====================

// GetProxyStatus 获取代理状态
func (a *App) GetProxyStatus() service.ProxyStats {
	stats := a.proxyService.GetStatus()
	a.stateMu.RLock()
	stats.ProfileID = a.httpProfileID
	a.stateMu.RUnlock()
	return stats
}

// StartProxy 启动代理服务
func (a *App) StartProxy(profileID string) error {
	a.beginAction()
	defer a.endAction()
	return a.startProxy(profileID)
}
func (a *App) startProxy(profileID string) error {
	if a.quitting.Load() {
		return fmt.Errorf("application is quitting")
	}
	if a.configService == nil {
		return fmt.Errorf("配置服务未初始化")
	}

	profile, err := a.configService.GetProfile(profileID)
	if err != nil {
		a.AddLog("ERROR", fmt.Sprintf("获取配置失败: %v", err))
		return err
	}

	a.AddLog("INFO", fmt.Sprintf("正在启动代理服务，配置: %s", profile.Name))

	// 转换配置
	cfg := a.configService.ProfileToConfig(profile)
	switch a.GetSettings().SystemProxyMode {
	case "auto":
		cfg.SystemProxy = true
	case "manual":
		cfg.SystemProxy = false
	}

	// 启动代理
	if err := a.proxyService.StartWithConfig(cfg); err != nil {
		a.AddLog("ERROR", fmt.Sprintf("启动代理失败: %v", err))
		return err
	}

	// 更新最后使用时间
	a.setActiveProfile(profileID, false)
	a.configService.SetLastUsed(profileID)

	a.AddLog("INFO", fmt.Sprintf("代理服务已启动，HTTP: %s, SOCKS5: %s", cfg.ListenAddr, cfg.SocksAddr))
	return nil
}

// StopProxy 停止代理服务
func (a *App) StopProxy() error {
	a.beginAction()
	defer a.endAction()
	return a.stopProxy()
}
func (a *App) stopProxy() error {
	a.AddLog("INFO", "正在停止代理服务...")
	err := a.proxyService.Stop()
	if err != nil {
		a.AddLog("ERROR", fmt.Sprintf("停止代理失败: %v", err))
	} else {
		a.AddLog("INFO", "代理服务已停止")
	}
	return err
}

// RestartProxy 重启代理服务
func (a *App) RestartProxy() error {
	a.beginAction()
	defer a.endAction()
	a.AddLog("INFO", "正在重启代理服务...")
	id := a.GetProxyStatus().ProfileID
	if _, err := a.GetProfile(id); err != nil {
		return err
	}
	if err := a.stopProxy(); err != nil {
		return err
	}
	return a.startProxy(id)
}

// ==================== TUN服务相关方法 ====================

// GetTunStatus 获取TUN服务状态
func (a *App) GetTunStatus() service.TunStats {
	stats := a.tunService.GetStatus()
	a.stateMu.RLock()
	stats.ProfileID = a.tunProfileID
	a.stateMu.RUnlock()
	return stats
}

// StartTunProxy 启动TUN代理服务
func (a *App) StartTunProxy(profileID string) error {
	a.beginAction()
	defer a.endAction()
	return a.startTunProxy(profileID)
}
func (a *App) startTunProxy(profileID string) error {
	if a.quitting.Load() {
		return fmt.Errorf("application is quitting")
	}
	if a.configService == nil {
		return fmt.Errorf("配置服务未初始化")
	}

	profile, err := a.configService.GetProfile(profileID)
	if err != nil {
		a.AddLog("ERROR", fmt.Sprintf("获取配置失败: %v", err))
		return err
	}

	if !profile.TunMode {
		return fmt.Errorf("该配置未启用TUN模式")
	}

	a.AddLog("INFO", fmt.Sprintf("正在启动TUN服务，配置: %s", profile.Name))

	// 检查是否需要提权
	if a.tunService.RequiresElevation() {
		a.AddLog("WARN", "TUN模式需要管理员权限，请求提权...")
		return fmt.Errorf("需要管理员权限，请使用提权重启功能")
	}

	// 设置TUN配置
	tunCfg := &service.TunConfig{
		TunMode:   profile.TunMode,
		TunGlobal: profile.TunGlobal,
		TunCIDR:   profile.TunCIDR,
		TunRoutes: profile.TunRoutes,
		TunNAT:    profile.TunNAT,
	}
	a.tunService.SetConfig(tunCfg)

	// 设置完整配置(包含SSH信息)
	fullCfg := a.configService.ProfileToConfig(profile)
	a.tunService.SetFullConfig(fullCfg)

	// 启动TUN服务
	if err := a.tunService.Start(); err != nil {
		a.AddLog("ERROR", fmt.Sprintf("启动TUN服务失败: %v", err))
		return err
	}

	// 更新最后使用时间
	a.setActiveProfile(profileID, true)
	a.configService.SetLastUsed(profileID)

	mode := "路由模式"
	if profile.TunGlobal {
		mode = "全局模式"
	}
	a.AddLog("INFO", fmt.Sprintf("TUN服务已启动 (%s)", mode))
	return nil
}

// StopTunProxy 停止TUN代理服务
func (a *App) StopTunProxy() error {
	a.beginAction()
	defer a.endAction()
	return a.stopTunProxy()
}
func (a *App) stopTunProxy() error {
	a.AddLog("INFO", "正在停止TUN服务...")
	err := a.tunService.Stop()
	if err != nil {
		a.AddLog("ERROR", fmt.Sprintf("停止TUN服务失败: %v", err))
	} else {
		a.AddLog("INFO", "TUN服务已停止")
	}
	return err
}

// RequiresTunElevation 检查TUN模式是否需要提权
func (a *App) RequiresTunElevation() bool {
	return a.tunService.RequiresElevation()
}

// RequestElevationRestart 请求以管理员权限重启应用
func (a *App) RequestElevationRestart() error {
	a.AddLog("INFO", "正在请求管理员权限重启...")

	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取可执行文件路径失败: %v", err)
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		// macOS 使用 osascript 进行提权
		// 重要：必须直接执行二进制文件而不是使用 open 命令
		// 因为 open 命令启动的应用不会继承 root 权限
		// do shell script ... with administrator privileges 会以 root 身份执行命令

		// 不使用 nohup (会阻止 GUI 显示)，使用子 shell 和 & 后台执行
		// ( ... ) & 创建独立的后台子 shell
		script := fmt.Sprintf(`do shell script "('%s' &)" with administrator privileges`, execPath)
		cmd = exec.Command("osascript", "-e", script)

		a.AddLog("INFO", "等待用户授权...")
		a.AddLog("DEBUG", fmt.Sprintf("执行脚本: %s", script))
		a.AddLog("DEBUG", fmt.Sprintf("可执行文件路径: %s", execPath))

		// 在新 goroutine 中运行，避免阻塞 UI
		go func() {
			output, err := cmd.CombinedOutput()
			if err != nil {
				a.AddLog("ERROR", fmt.Sprintf("提权失败: %v, 输出: %s", err, string(output)))
				return
			}
			a.AddLog("INFO", "提权成功，正在退出当前实例...")
			// 给新进程足够时间启动
			time.Sleep(1 * time.Second)
			a.QuitApp()
		}()
		return nil

	case "linux":
		// Linux 优先使用 pkexec
		if _, err := exec.LookPath("pkexec"); err == nil {
			cmd = exec.Command("pkexec", execPath)
		} else {
			// 使用 xterm + sudo 作为回退
			cmd = exec.Command("xterm", "-e", "sudo", execPath)
		}
	case "windows":
		// Windows 使用 PowerShell 以管理员权限启动
		cmd = exec.Command("powershell", "-Command",
			fmt.Sprintf("Start-Process -FilePath '%s' -Verb RunAs", execPath))
	default:
		return fmt.Errorf("不支持的操作系统: %s", runtime.GOOS)
	}

	// 启动提权进程（Linux/Windows）
	if err := cmd.Start(); err != nil {
		a.AddLog("ERROR", fmt.Sprintf("启动提权进程失败: %v", err))
		return fmt.Errorf("启动提权进程失败: %v", err)
	}

	a.AddLog("INFO", "提权进程已启动，正在退出当前实例...")

	// 退出当前应用
	go func() {
		time.Sleep(500 * time.Millisecond)
		a.QuitApp()
	}()

	return nil
}

// IsRunningAsAdmin 检查是否以管理员权限运行
func (a *App) IsRunningAsAdmin() bool {
	return !a.tunService.RequiresElevation()
}

// TestConnection 测试SSH连接
func (a *App) TestConnection(profileID string) error {
	if a.configService == nil {
		return fmt.Errorf("配置服务未初始化")
	}

	profile, err := a.configService.GetProfile(profileID)
	if err != nil {
		a.AddLog("ERROR", fmt.Sprintf("获取配置失败: %v", err))
		return err
	}

	a.AddLog("INFO", fmt.Sprintf("正在测试SSH连接: %s@%s:%s", profile.User, profile.Host, profile.Port))

	cfg := a.configService.ProfileToConfig(profile)
	err = a.proxyService.TestConnection(cfg)
	if err != nil {
		a.AddLog("ERROR", fmt.Sprintf("SSH连接测试失败: %v", err))
		return err
	}

	a.AddLog("INFO", fmt.Sprintf("SSH连接测试成功: %s@%s:%s", profile.User, profile.Host, profile.Port))
	return nil
}

// TestConnectionWithConfig 使用临时配置测试连接
func (a *App) TestConnectionWithConfig(host, port, user, password, keyFile string) error {
	profile := &service.SSHProfile{
		Host:     host,
		Port:     port,
		User:     user,
		Password: password,
		KeyFile:  keyFile,
		HTTPAddr: ":8080",
	}
	cfg := a.configService.ProfileToConfig(profile)
	return a.proxyService.TestConnection(cfg)
}

// ==================== 配置管理相关方法 ====================

// GetSettings 获取应用设置
func (a *App) GetSettings() service.AppSettings {
	if a.configService == nil {
		return service.AppSettings{}
	}
	return a.configService.GetSettings()
}

// UpdateSettings 更新应用设置
func (a *App) UpdateSettings(settings service.AppSettings) error {
	a.beginAction()
	defer a.endAction()
	if a.configService == nil {
		return fmt.Errorf("配置服务未初始化")
	}
	// The native menu owns this preference; stale frontend settings must not overwrite it.
	settings.SystemProxyMode = a.GetSettings().SystemProxyMode
	return a.configService.UpdateSettings(settings)
}

// GetProfiles 获取所有配置文件
func (a *App) GetProfiles() []service.SSHProfile {
	if a.configService == nil {
		return []service.SSHProfile{}
	}
	return a.configService.GetProfiles()
}

// GetProfile 获取指定配置文件
func (a *App) GetProfile(id string) (*service.SSHProfile, error) {
	if a.configService == nil {
		return nil, fmt.Errorf("配置服务未初始化")
	}
	return a.configService.GetProfile(id)
}

// AddProfile 添加配置文件
func (a *App) AddProfile(profile service.SSHProfile) error {
	if a.configService == nil {
		return fmt.Errorf("配置服务未初始化")
	}
	return a.configService.AddProfile(profile)
}

// UpdateProfile 更新配置文件
func (a *App) UpdateProfile(profile service.SSHProfile) error {
	if a.configService == nil {
		return fmt.Errorf("配置服务未初始化")
	}
	return a.configService.UpdateProfile(profile)
}

// DeleteProfile 删除配置文件
func (a *App) DeleteProfile(id string) error {
	if a.configService == nil {
		return fmt.Errorf("配置服务未初始化")
	}
	return a.configService.DeleteProfile(id)
}

// GetDefaultProfile 获取默认配置文件
func (a *App) GetDefaultProfile() *service.SSHProfile {
	if a.configService == nil {
		return nil
	}
	return a.configService.GetDefaultProfile()
}

// ==================== 系统相关方法 ====================

// GetVersion 获取版本号
func (a *App) GetVersion() string {
	return Version
}

// OpenFileDialog 打开文件选择对话框
func (a *App) OpenFileDialog(title string, filters []string) (string, error) {
	options := wailsRuntime.OpenDialogOptions{
		Title: title,
	}
	return wailsRuntime.OpenFileDialog(a.ctx, options)
}

// OpenDirectoryDialog 打开目录选择对话框
func (a *App) OpenDirectoryDialog(title string) (string, error) {
	options := wailsRuntime.OpenDialogOptions{
		Title: title,
	}
	return wailsRuntime.OpenDirectoryDialog(a.ctx, options)
}

// SaveFileDialog 打开保存文件对话框
func (a *App) SaveFileDialog(title string, defaultFilename string) (string, error) {
	options := wailsRuntime.SaveDialogOptions{
		Title:           title,
		DefaultFilename: defaultFilename,
	}
	return wailsRuntime.SaveFileDialog(a.ctx, options)
}

// SaveLogsToFile 保存日志到指定文件
func (a *App) SaveLogsToFile(filePath string) error {
	a.logMu.RLock()
	logs := make([]LogEntry, len(a.logs))
	copy(logs, a.logs)
	a.logMu.RUnlock()

	var content string
	for _, log := range logs {
		content += fmt.Sprintf("[%s] [%s] %s\n", log.Timestamp, log.Level, log.Message)
	}

	return os.WriteFile(filePath, []byte(content), 0644)
}

// ==================== 日志相关方法 ====================

// AddLog 添加日志条目
func (a *App) AddLog(level, message string) {
	a.logMu.Lock()
	defer a.logMu.Unlock()

	a.logCounter++
	entry := LogEntry{
		ID:        a.logCounter,
		Timestamp: time.Now().Format(time.RFC3339),
		Level:     level,
		Message:   message,
	}

	a.logs = append(a.logs, entry)

	// 限制日志数量
	if len(a.logs) > a.maxLogs {
		a.logs = a.logs[len(a.logs)-a.maxLogs:]
	}

	// 发送事件到前端
	if a.ctx != nil {
		wailsRuntime.EventsEmit(a.ctx, "log:new", entry)
	}
}

// GetLogs 获取所有日志
func (a *App) GetLogs() []LogEntry {
	a.logMu.RLock()
	defer a.logMu.RUnlock()

	// 返回副本
	result := make([]LogEntry, len(a.logs))
	copy(result, a.logs)
	return result
}

// GetLogsSince 获取指定ID之后的日志
func (a *App) GetLogsSince(lastID int64) []LogEntry {
	a.logMu.RLock()
	defer a.logMu.RUnlock()

	var result []LogEntry
	for _, log := range a.logs {
		if log.ID > lastID {
			result = append(result, log)
		}
	}
	return result
}

// ClearLogs 清空日志
func (a *App) ClearLogs() {
	a.logMu.Lock()
	a.logs = make([]LogEntry, 0, 1000)
	a.logMu.Unlock()

	a.AddLog("INFO", "日志已清空")
}

// OpenURL 在默认浏览器中打开URL
func (a *App) OpenURL(url string) {
	wailsRuntime.BrowserOpenURL(a.ctx, url)
}

// SetAutoStart 设置开机自启动
func (a *App) SetAutoStart(enabled bool) error {
	a.beginAction()
	defer a.endAction()
	// 更新设置
	if a.configService == nil {
		return fmt.Errorf("配置服务未初始化")
	}
	settings := a.configService.GetSettings()
	settings.AutoStart = enabled
	if err := a.configService.UpdateSettings(settings); err != nil {
		return err
	}

	// macOS: 使用 launchctl 管理登录项
	// 这里只是更新设置，实际的 launchd plist 需要手动创建或使用专门的库
	// 对于 wails 应用，可以考虑使用 github.com/emersion/go-autostart
	a.AddLog("INFO", fmt.Sprintf("开机自启动设置已更新: %v", enabled))
	return nil
}

// SetMinimizeToTray 设置关闭时最小化到托盘
func (a *App) SetMinimizeToTray(enabled bool) error {
	a.beginAction()
	defer a.endAction()
	// 更新设置
	if a.configService == nil {
		return fmt.Errorf("配置服务未初始化")
	}
	settings := a.configService.GetSettings()
	settings.MinimizeToTray = enabled
	if err := a.configService.UpdateSettings(settings); err != nil {
		return err
	}
	a.AddLog("INFO", fmt.Sprintf("最小化到托盘设置已更新: %v", enabled))
	return nil
}

// HideWindow 隐藏窗口
func (a *App) HideWindow() {
	wailsRuntime.WindowHide(a.ctx)
}

// ShowWindow 显示窗口
func (a *App) ShowWindow() {
	wailsRuntime.WindowShow(a.ctx)
	wailsRuntime.WindowUnminimise(a.ctx)
}

// QuitApp 退出应用
func (a *App) QuitApp() {
	a.quitting.Store(true)
	if a.ctx != nil {
		wailsRuntime.Quit(a.ctx)
	}
}
