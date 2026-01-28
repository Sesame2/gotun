package main

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/Sesame2/gotun/internal/service"
	"github.com/wailsapp/wails/v2/pkg/runtime"
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
	ctx           context.Context
	proxyService  *service.ProxyService
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
		proxyService: service.NewProxyService(),
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
		runtime.LogError(ctx, fmt.Sprintf("初始化配置服务失败: %v", err))
		a.AddLog("ERROR", fmt.Sprintf("初始化配置服务失败: %v", err))
	} else {
		a.configService = configSvc
		a.AddLog("INFO", "配置服务初始化成功")
	}

	// 设置代理服务的日志回调
	a.proxyService.SetLogCallback(func(level, message string) {
		a.AddLog(level, message)
	})

	// 应用配置
	settings := a.configService.GetSettings()
	a.proxyService.SetVerbose(settings.Verbose)
	if settings.LogFile != "" {
		a.proxyService.SetLogFile(settings.LogFile)
	}

	a.AddLog("INFO", "GoTun GUI 启动完成")
}

// shutdown is called when the app is closing
func (a *App) shutdown(ctx context.Context) {
	// 停止代理服务
	if a.proxyService.IsRunning() {
		a.proxyService.Stop()
	}
}

// ==================== 代理服务相关方法 ====================

// GetProxyStatus 获取代理状态
func (a *App) GetProxyStatus() service.ProxyStats {
	return a.proxyService.GetStatus()
}

// StartProxy 启动代理服务
func (a *App) StartProxy(profileID string) error {
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
	a.proxyService.SetConfig(cfg)

	// 启动代理
	if err := a.proxyService.Start(); err != nil {
		a.AddLog("ERROR", fmt.Sprintf("启动代理失败: %v", err))
		return err
	}

	// 更新最后使用时间
	a.configService.SetLastUsed(profileID)

	a.AddLog("INFO", fmt.Sprintf("代理服务已启动，HTTP: %s, SOCKS5: %s", cfg.ListenAddr, cfg.SocksAddr))
	return nil
}

// StopProxy 停止代理服务
func (a *App) StopProxy() error {
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
	a.AddLog("INFO", "正在重启代理服务...")
	return a.proxyService.Restart()
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
	if a.configService == nil {
		return fmt.Errorf("配置服务未初始化")
	}
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
	options := runtime.OpenDialogOptions{
		Title: title,
	}
	return runtime.OpenFileDialog(a.ctx, options)
}

// OpenDirectoryDialog 打开目录选择对话框
func (a *App) OpenDirectoryDialog(title string) (string, error) {
	options := runtime.OpenDialogOptions{
		Title: title,
	}
	return runtime.OpenDirectoryDialog(a.ctx, options)
}

// SaveFileDialog 打开保存文件对话框
func (a *App) SaveFileDialog(title string, defaultFilename string) (string, error) {
	options := runtime.SaveDialogOptions{
		Title:           title,
		DefaultFilename: defaultFilename,
	}
	return runtime.SaveFileDialog(a.ctx, options)
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
		runtime.EventsEmit(a.ctx, "log:new", entry)
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
	runtime.BrowserOpenURL(a.ctx, url)
}

// SetAutoStart 设置开机自启动
func (a *App) SetAutoStart(enabled bool) error {
	// 更新设置
	if a.configService != nil {
		settings := a.configService.GetSettings()
		settings.AutoStart = enabled
		if err := a.configService.UpdateSettings(settings); err != nil {
			return err
		}
	}

	// macOS: 使用 launchctl 管理登录项
	// 这里只是更新设置，实际的 launchd plist 需要手动创建或使用专门的库
	// 对于 wails 应用，可以考虑使用 github.com/emersion/go-autostart
	a.AddLog("INFO", fmt.Sprintf("开机自启动设置已更新: %v", enabled))
	return nil
}

// SetMinimizeToTray 设置关闭时最小化到托盘
func (a *App) SetMinimizeToTray(enabled bool) error {
	// 更新设置
	if a.configService != nil {
		settings := a.configService.GetSettings()
		settings.MinimizeToTray = enabled
		if err := a.configService.UpdateSettings(settings); err != nil {
			return err
		}
	}
	a.AddLog("INFO", fmt.Sprintf("最小化到托盘设置已更新: %v", enabled))
	return nil
}

// HideWindow 隐藏窗口
func (a *App) HideWindow() {
	runtime.WindowHide(a.ctx)
}

// ShowWindow 显示窗口
func (a *App) ShowWindow() {
	runtime.WindowShow(a.ctx)
}

// QuitApp 退出应用
func (a *App) QuitApp() {
	runtime.Quit(a.ctx)
}
