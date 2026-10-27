package main

import (
	"context"
	"fmt"
	"runtime"

	"github.com/Sesame2/gotun/internal/config"
	"github.com/Sesame2/gotun/internal/service"
	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

func (a *App) connectionConfig(profile *service.SSHProfile) (*config.Config, func()) {
	parent := a.ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	a.connectionMu.Lock()
	a.connectionID++
	id := a.connectionID
	if a.connections == nil {
		a.connections = make(map[uint64]context.CancelFunc)
	}
	a.connections[id] = cancel
	a.connectionMu.Unlock()
	cfg := a.configService.ProfileToConfig(profile)
	cfg.Context = ctx
	cfg.HostKeyPrompt = a.hostKeyPrompt
	if cfg.HostKeyPrompt == nil {
		cfg.HostKeyPrompt = a.confirmHostKey
	}
	return cfg, func() {
		cancel()
		a.connectionMu.Lock()
		delete(a.connections, id)
		a.connectionMu.Unlock()
	}
}

func (a *App) cancelConnections() {
	a.connectionMu.Lock()
	defer a.connectionMu.Unlock()
	for _, cancel := range a.connections {
		cancel()
	}
}

func (a *App) confirmHostKey(host, fingerprint string) bool {
	if a.ctx == nil {
		return false
	}
	options := hostKeyDialogOptions(runtime.GOOS, a.GetSettings().Language, host, fingerprint)
	answer, err := wailsRuntime.MessageDialog(a.ctx, options)
	return err == nil && answer == options.Buttons[0]
}

func hostKeyDialogOptions(platform, language, host, fingerprint string) wailsRuntime.MessageDialogOptions {
	title, message, trust, cancel := "确认 SSH 服务器身份", "首次连接 %s。\n\n服务器指纹：\n%s\n\n请通过可信渠道核对指纹，确认后才会发送认证信息。是否信任并保存？", "信任", "取消"
	if language == "en-US" {
		title, message, trust, cancel = "Verify SSH host", "First connection to %s.\n\nHost fingerprint:\n%s\n\nVerify this fingerprint through a trusted channel before authentication. Trust and save this identity?", "Trust", "Cancel"
	}
	if platform == "windows" {
		// Wails uses native MB_YESNO and returns these names on every Windows locale.
		trust, cancel = "Yes", "No"
	}
	return wailsRuntime.MessageDialogOptions{
		Type: wailsRuntime.QuestionDialog, Title: title, Message: fmt.Sprintf(message, host, fingerprint),
		Buttons: []string{trust, cancel}, DefaultButton: cancel, CancelButton: cancel,
	}
}
