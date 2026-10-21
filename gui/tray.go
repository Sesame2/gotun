package main

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/Sesame2/gotun/gui/internal/tray"
	"github.com/Sesame2/gotun/internal/service"
	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type trayState struct {
	language           string
	profiles           []service.SSHProfile
	activeID           string
	running, busy      bool
	mode, errorMessage string
}

func buildTrayMenu(s trayState) []tray.Item {
	en := s.language == "en-US"
	text := func(zh, enText string) string {
		if en {
			return enText
		}
		return zh
	}
	profiles := make([]tray.Item, 0, len(s.profiles))
	for _, p := range s.profiles {
		profiles = append(profiles, tray.Item{ID: "profile:" + p.ID, Label: p.Name, Checked: s.running && p.ID == s.activeID, Disabled: s.busy})
	}
	mode := s.mode
	if mode == "" {
		mode = "follow-profile"
	}
	statusLabel := text("已停止", "Stopped")
	if s.running {
		statusLabel = text("运行中", "Running")
	}
	if s.busy {
		statusLabel = text("正在处理…", "Working...")
	}
	if s.errorMessage != "" {
		statusLabel = s.errorMessage
	}
	items := []tray.Item{
		{ID: "status", Label: statusLabel, Disabled: true},
		{ID: "show", Label: text("显示主界面", "Show main window")},
		{ID: "hide", Label: text("隐藏主界面", "Hide main window")},
		{ID: "profiles", Label: text("配置", "Profiles"), Children: profiles, Disabled: s.busy || len(profiles) == 0},
		{ID: "stop", Label: text("停止代理", "Stop proxy"), Disabled: s.busy || (!s.running && s.errorMessage == "")},
		{ID: "clear", Label: text("清除系统代理", "Clear system proxy"), Disabled: s.busy},
		{ID: "mode:follow-profile", Label: text("遵循配置的系统代理设置", "Follow profile proxy settings"), Checked: mode == "follow-profile", Disabled: s.busy},
		{ID: "mode:auto", Label: text("自动配置系统代理", "Automatically configure system proxy"), Checked: mode == "auto", Disabled: s.busy},
		{ID: "mode:manual", Label: text("不改变系统代理", "Do not change system proxy"), Checked: mode == "manual", Disabled: s.busy},
		{ID: "quit", Label: text("退出 GoTun", "Quit GoTun")},
	}
	return items
}

func (a *App) traySnapshot() trayState {
	settings := a.GetSettings()
	http := a.GetProxyStatus()
	tun := a.GetTunStatus()
	running := http.Status == service.StatusRunning || tun.Status == service.TunStatusRunning
	active := http.ProfileID
	if tun.Status == service.TunStatusRunning {
		active = tun.ProfileID
	}
	message := http.ErrorMessage
	if tun.ErrorMessage != "" {
		message = tun.ErrorMessage
	}
	return trayState{language: settings.Language, profiles: a.GetProfiles(), activeID: active, running: running, busy: a.actionBusy.Load(), mode: settings.SystemProxyMode, errorMessage: message}
}

func (a *App) startTray(ctx context.Context) { a.trayInit.Do(func() { a.initTray(ctx) }) }
func (a *App) initTray(ctx context.Context) {
	native := tray.New()
	if err := native.Start(a.handleTrayAction); err != nil {
		a.AddLog("ERROR", fmt.Sprintf("初始化托盘失败: %v", err))
		return
	}
	a.trayMu.Lock()
	defer a.trayMu.Unlock()
	if a.quitting.Load() {
		native.Close()
		return
	}
	a.tray = native
	native.Update(buildTrayMenu(a.traySnapshot()))
	a.trayReady.Store(true)
	a.AddLog("INFO", "System tray is ready")
	wailsRuntime.MenuSetApplicationMenu(a.ctx, a.applicationMenu())
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		var previous []tray.Item
		language := a.GetSettings().Language
		for {
			select {
			case <-ctx.Done():
				return
			case <-a.trayDone:
				return
			case <-ticker.C:
			case <-a.trayUpdates:
			}
			currentLanguage := a.GetSettings().Language
			if currentLanguage != language {
				wailsRuntime.MenuSetApplicationMenu(a.ctx, a.applicationMenu())
				language = currentLanguage
			}
			items := buildTrayMenu(a.traySnapshot())
			if !reflect.DeepEqual(previous, items) {
				native.Update(items)
				previous = items
			}
		}
	}()
}
func (a *App) refreshTray() {
	select {
	case a.trayUpdates <- struct{}{}:
	default:
	}
}
func (a *App) handleTrayAction(id string) {
	switch id {
	case "show":
		a.ShowWindow()
		return
	case "hide":
		a.HideWindow()
		return
	case "quit":
		a.QuitApp()
		return
	}
	if a.quitting.Load() {
		return
	}
	var err error
	switch {
	case strings.HasPrefix(id, "profile:"):
		err = a.selectTrayProfile(strings.TrimPrefix(id, "profile:"))
	case strings.HasPrefix(id, "mode:"):
		err = a.setSystemProxyMode(strings.TrimPrefix(id, "mode:"))
	case id == "stop":
		err = a.stopAllProxies()
	case id == "clear":
		err = a.clearSystemProxy()
	default:
		return
	}
	a.refreshTray()
	if err != nil {
		a.AddLog("ERROR", err.Error())
		if a.ctx != nil && !a.quitting.Load() {
			a.ShowWindow()
			title := "GoTun 操作失败"
			if a.GetSettings().Language == "en-US" {
				title = "GoTun action failed"
			}
			wailsRuntime.MessageDialog(a.ctx, wailsRuntime.MessageDialogOptions{Type: wailsRuntime.ErrorDialog, Title: title, Message: err.Error()})
		}
	}
}

func (a *App) beginAction() { a.actionMu.Lock(); a.actionBusy.Store(true); a.refreshTray() }
func (a *App) endAction()   { a.actionBusy.Store(false); a.actionMu.Unlock(); a.refreshTray() }
func (a *App) setActiveProfile(id string, tun bool) {
	a.stateMu.Lock()
	if tun {
		a.tunProfileID = id
	} else {
		a.httpProfileID = id
	}
	a.stateMu.Unlock()
}

func (a *App) selectTrayProfile(id string) error {
	a.beginAction()
	defer a.endAction()
	if a.quitting.Load() {
		return fmt.Errorf("application is quitting")
	}
	p, err := a.GetProfile(id)
	if err != nil {
		return err
	}
	port, err := strconv.Atoi(p.Port)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("无效的 SSH 端口: %s", p.Port)
	}
	cfg := a.configService.ProfileToConfig(p)
	if err := cfg.Validate(); err != nil {
		return err
	}
	if p.TunMode && a.RequiresTunElevation() {
		return fmt.Errorf("TUN 模式需要管理员权限，请在主界面使用提权重启")
	}
	status := a.GetProxyStatus()
	tun := a.GetTunStatus()
	if (!p.TunMode && status.Status == service.StatusRunning && status.ProfileID == id) || (p.TunMode && tun.Status == service.TunStatusRunning && tun.ProfileID == id) {
		return nil
	}
	if err := a.stopAllProxiesLocked(); err != nil {
		return err
	}
	if p.TunMode {
		return a.startTunProxy(id)
	}
	return a.startProxy(id)
}
func (a *App) stopAllProxiesLocked() error {
	return errors.Join(a.stopTunProxy(), a.stopProxy())
}
func (a *App) stopAllProxies() error {
	a.beginAction()
	defer a.endAction()
	return a.stopAllProxiesLocked()
}
func (a *App) clearSystemProxy() error {
	a.beginAction()
	defer a.endAction()
	return a.proxyService.ClearSystemProxy()
}

func (a *App) setSystemProxyMode(mode string) error {
	if mode != "auto" && mode != "manual" && mode != "follow-profile" {
		return fmt.Errorf("invalid system proxy mode: %s", mode)
	}
	a.beginAction()
	defer a.endAction()
	if a.configService == nil {
		return fmt.Errorf("配置服务未初始化")
	}
	wasEnabled := a.GetProxyStatus().SystemProxy
	enabled := false
	if mode == "auto" {
		enabled = true
	} else if mode == "follow-profile" {
		status := a.GetProxyStatus()
		if status.ProfileID != "" {
			p, err := a.GetProfile(status.ProfileID)
			if err != nil {
				return err
			}
			enabled = p.SystemProxy
		}
	}
	if !enabled || a.proxyService.IsRunning() {
		if err := a.proxyService.SetSystemProxy(enabled); err != nil {
			return err
		}
	}
	settings := a.GetSettings()
	settings.SystemProxyMode = mode
	if err := a.configService.UpdateSettings(settings); err != nil {
		if rollbackErr := a.proxyService.SetSystemProxy(wasEnabled); rollbackErr != nil {
			return fmt.Errorf("保存失败: %v; 恢复系统代理失败: %w", err, rollbackErr)
		}
		return err
	}
	return nil
}

func (a *App) shouldHideOnClose() bool {
	return a.trayReady.Load() && !a.quitting.Load() && a.GetSettings().MinimizeToTray
}
func (a *App) beforeClose(ctx context.Context) bool {
	if a.shouldHideOnClose() {
		wailsRuntime.WindowHide(ctx)
		return true
	}
	return false
}
