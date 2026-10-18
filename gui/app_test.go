package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Sesame2/gotun/internal/service"
	"github.com/Sesame2/gotun/internal/testutil"
)

func newTestApp(t *testing.T) *App {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	app := NewApp()
	var err error
	app.configService, err = service.NewConfigService()
	if err != nil {
		t.Fatal(err)
	}
	return app
}

func TestNewApp(t *testing.T) {
	app := NewApp()
	if app.proxyService == nil || app.tunService == nil || app.logs == nil || app.maxLogs != 1000 {
		t.Fatalf("unexpected app defaults: %+v", app)
	}
	if got := app.GetVersion(); got != Version || got == "" {
		t.Fatalf("version = %q, want %q", got, Version)
	}
	if status := app.GetProxyStatus(); status.Status != service.StatusStopped {
		t.Fatalf("initial proxy status = %+v", status)
	}
	if status := app.GetTunStatus(); status.Status != "stopped" {
		t.Fatalf("initial TUN status = %+v", status)
	}
	if app.IsRunningAsAdmin() == app.RequiresTunElevation() {
		t.Fatal("admin and elevation checks must be complementary")
	}
	if err := app.StopProxy(); err != nil {
		t.Fatalf("stopping an idle proxy: %v", err)
	}
	if err := app.StopTunProxy(); err != nil {
		t.Fatalf("stopping an idle TUN proxy: %v", err)
	}
	app.shutdown(nil)
}

func TestAppConfigurationAPI(t *testing.T) {
	app := newTestApp(t)
	profile := service.SSHProfile{ID: "primary", Name: "Primary", Host: "localhost", User: "alice", Password: "secret", JumpHosts: []string{"jump@localhost"}}
	if err := app.AddProfile(profile); err != nil {
		t.Fatal(err)
	}
	profiles := app.GetProfiles()
	if len(profiles) != 1 || profiles[0].Port != "22" || profiles[0].HTTPAddr != ":8080" {
		t.Fatalf("unexpected profiles: %+v", profiles)
	}
	profiles[0].Name = "Local only"
	profiles[0].JumpHosts[0] = "changed"
	stored, err := app.GetProfile(profile.ID)
	if err != nil || stored.Name != "Primary" || stored.JumpHosts[0] != "jump@localhost" {
		t.Fatalf("profile result changed app state: %+v, %v", stored, err)
	}
	stored.Name = "Renamed"
	if err := app.UpdateProfile(*stored); err != nil {
		t.Fatal(err)
	}
	settings := app.GetSettings()
	settings.DefaultProfile = stored.ID
	settings.Language = "en-US"
	settings.Theme = "dark"
	if err := app.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}
	if err := app.SetAutoStart(true); err != nil {
		t.Fatal(err)
	}
	if err := app.SetMinimizeToTray(false); err != nil {
		t.Fatal(err)
	}
	reloaded, err := service.NewConfigService()
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.GetSettings(); !got.AutoStart || got.MinimizeToTray || got.Language != "en-US" || got.Theme != "dark" {
		t.Fatalf("GUI settings were not persisted: %+v", got)
	}
	if got := app.GetDefaultProfile(); got == nil || got.ID != stored.ID || got.Name != "Renamed" {
		t.Fatalf("default profile = %+v", got)
	}
	if got, err := reloaded.GetProfile(stored.ID); err != nil || got.Name != "Renamed" {
		t.Fatalf("GUI update was not persisted: %+v, %v", got, err)
	}
	if err := app.DeleteProfile(stored.ID); err != nil {
		t.Fatal(err)
	}
	if len(app.GetProfiles()) != 0 || app.GetDefaultProfile() != nil {
		t.Fatal("deleted profile remains visible")
	}
}

func TestAppWithoutConfiguration(t *testing.T) {
	app := NewApp()
	if got := app.GetProfiles(); got == nil || len(got) != 0 {
		t.Fatalf("uninitialized profiles = %#v", got)
	}
	if got := app.GetSettings(); got != (service.AppSettings{}) {
		t.Fatalf("uninitialized settings = %+v", got)
	}
	if app.GetDefaultProfile() != nil {
		t.Fatal("uninitialized app has a default profile")
	}
	if _, err := app.GetProfile("missing"); err == nil {
		t.Fatal("uninitialized profile lookup succeeded")
	}
	operations := map[string]func() error{
		"start proxy":      func() error { return app.StartProxy("missing") },
		"start TUN":        func() error { return app.StartTunProxy("missing") },
		"test connection":  func() error { return app.TestConnection("missing") },
		"update settings":  func() error { return app.UpdateSettings(service.AppSettings{}) },
		"add profile":      func() error { return app.AddProfile(service.SSHProfile{}) },
		"update profile":   func() error { return app.UpdateProfile(service.SSHProfile{}) },
		"delete profile":   func() error { return app.DeleteProfile("missing") },
		"auto start":       func() error { return app.SetAutoStart(true) },
		"minimize to tray": func() error { return app.SetMinimizeToTray(true) },
	}
	for name, operation := range operations {
		t.Run(name, func(t *testing.T) {
			if err := operation(); err == nil || !strings.Contains(err.Error(), "配置服务未初始化") {
				t.Fatalf("want uninitialized configuration error, got %v", err)
			}
		})
	}
}

func TestAppInvalidProfiles(t *testing.T) {
	app := newTestApp(t)
	for name, operation := range map[string]func() error{
		"start proxy":     func() error { return app.StartProxy("missing") },
		"start TUN":       func() error { return app.StartTunProxy("missing") },
		"test connection": func() error { return app.TestConnection("missing") },
		"update":          func() error { return app.UpdateProfile(service.SSHProfile{ID: "missing"}) },
		"delete":          func() error { return app.DeleteProfile("missing") },
	} {
		t.Run(name, func(t *testing.T) {
			if err := operation(); err == nil || !strings.Contains(err.Error(), "配置文件不存在") {
				t.Fatalf("want missing profile error, got %v", err)
			}
		})
	}
	if err := app.AddProfile(service.SSHProfile{ID: "disabled", Host: "localhost", User: "alice"}); err != nil {
		t.Fatal(err)
	}
	if err := app.StartTunProxy("disabled"); err == nil || !strings.Contains(err.Error(), "未启用TUN模式") {
		t.Fatalf("disabled TUN configuration was accepted: %v", err)
	}
	if err := app.AddProfile(service.SSHProfile{ID: "invalid"}); err != nil {
		t.Fatal(err)
	}
	if err := app.StartProxy("invalid"); err == nil {
		t.Fatal("proxy accepted a profile without an SSH username")
	}
	if status := app.GetProxyStatus(); status.Status != service.StatusError || status.ErrorMessage == "" {
		t.Fatalf("proxy error not available to GUI: %+v", status)
	}
	logs := app.GetLogs()
	if len(logs) == 0 || logs[len(logs)-1].Level != "ERROR" {
		t.Fatalf("proxy error was not logged: %+v", logs)
	}
}

func TestAppSSHConnectionChecks(t *testing.T) {
	app := newTestApp(t)
	server := testutil.NewSSHServer(t)
	host, port, err := net.SplitHostPort(server.Addr)
	if err != nil {
		t.Fatal(err)
	}
	profile := service.SSHProfile{ID: "ssh-test", Host: host, Port: port, User: "tester", KeyFile: server.KeyFile}
	if err := app.AddProfile(profile); err != nil {
		t.Fatal(err)
	}
	if err := app.TestConnection(profile.ID); err != nil {
		t.Fatalf("saved profile connection check failed: %v", err)
	}
	stored, err := app.GetProfile(profile.ID)
	if err != nil || !stored.LastUsedAt.IsZero() {
		t.Fatalf("connection check changed last-used state: %+v, %v", stored, err)
	}
	if err := app.TestConnectionWithConfig(host, port, "tester", "test-password", ""); err != nil {
		t.Fatalf("temporary password connection check failed: %v", err)
	}
	profile.KeyFile = filepath.Join(t.TempDir(), "missing-key")
	profile.Password = "wrong-password"
	if err := app.UpdateProfile(profile); err != nil {
		t.Fatal(err)
	}
	if err := app.TestConnection(profile.ID); err == nil {
		t.Fatal("saved profile accepted wrong SSH credentials")
	}
	logs := app.GetLogs()
	if logs[len(logs)-1].Level != "ERROR" || !strings.Contains(logs[len(logs)-1].Message, "SSH连接测试失败") {
		t.Fatalf("connection failure was not available to GUI: %+v", logs)
	}
	if err := app.TestConnectionWithConfig(host, port, "tester", "wrong-password", profile.KeyFile); err == nil {
		t.Fatal("temporary connection accepted wrong SSH credentials")
	}
}

func TestAppProxyLifecyclePersistsLastUsed(t *testing.T) {
	app := newTestApp(t)
	server := testutil.NewSSHServer(t)
	host, port, err := net.SplitHostPort(server.Addr)
	if err != nil {
		t.Fatal(err)
	}
	profile := service.SSHProfile{ID: "proxy", Host: host, Port: port, User: "tester", KeyFile: server.KeyFile, HTTPAddr: testutil.AvailableAddr(t)}
	if err := app.AddProfile(profile); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.StopProxy(); err != nil {
			t.Error(err)
		}
	})
	before := time.Now()
	if err := app.StartProxy(profile.ID); err != nil {
		t.Fatal(err)
	}
	testutil.WaitTCP(t, profile.HTTPAddr)
	status := app.GetProxyStatus()
	if status.Status != service.StatusRunning || status.SSHServer != server.Addr || status.SystemProxy {
		t.Fatalf("unexpected running proxy status: %+v", status)
	}
	reloaded, err := service.NewConfigService()
	if err != nil {
		t.Fatal(err)
	}
	stored, err := reloaded.GetProfile(profile.ID)
	if err != nil || stored.LastUsedAt.Before(before) || stored.LastUsedAt.After(time.Now()) {
		t.Fatalf("successful start did not persist last-used time: %+v, %v", stored, err)
	}
	if err := app.RestartProxy(); err != nil {
		t.Fatalf("restart failed: %v", err)
	}
	testutil.WaitTCP(t, profile.HTTPAddr)
	if status := app.GetProxyStatus(); status.Status != service.StatusRunning {
		t.Fatalf("proxy did not restart: %+v", status)
	}
	if err := app.StopProxy(); err != nil {
		t.Fatal(err)
	}
	if status := app.GetProxyStatus(); status.Status != service.StatusStopped {
		t.Fatalf("proxy did not stop: %+v", status)
	}
}

func TestAppTUNRequiresPrivileges(t *testing.T) {
	app := newTestApp(t)
	if !app.RequiresTunElevation() {
		t.Skip("this check must never start a TUN device when already privileged")
	}
	if err := app.AddProfile(service.SSHProfile{ID: "tun", TunMode: true}); err != nil {
		t.Fatal(err)
	}
	if err := app.StartTunProxy("tun"); err == nil || !strings.Contains(err.Error(), "需要管理员权限") {
		t.Fatalf("unprivileged TUN start should explain elevation requirement: %v", err)
	}
	if got := app.GetTunStatus(); got.Status != "stopped" {
		t.Fatalf("unprivileged start changed TUN status: %+v", got)
	}
}

func TestStartupLoadsConfiguration(t *testing.T) {
	app := newTestApp(t)
	if err := app.AddProfile(service.SSHProfile{ID: "saved", Name: "Saved"}); err != nil {
		t.Fatal(err)
	}
	app = NewApp()
	app.startup(nil)
	defer app.shutdown(nil)
	if got, err := app.GetProfile("saved"); err != nil || got.Name != "Saved" {
		t.Fatalf("startup did not load saved profiles: %+v, %v", got, err)
	}
	logs := app.GetLogs()
	if len(logs) != 2 || logs[0].Level != "INFO" || logs[1].Message != "GoTun GUI 启动完成" {
		t.Fatalf("unexpected startup logs: %+v", logs)
	}
}

func TestStartupConfigurationErrorKeepsAppUsable(t *testing.T) {
	app := newTestApp(t)
	configPath := filepath.Join(os.Getenv("HOME"), ".gotun", "config.json")
	if err := os.WriteFile(configPath, []byte("{invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	app = NewApp()
	app.startup(nil)
	defer app.shutdown(nil)
	if app.configService != nil {
		t.Fatal("failed initialization installed a configuration service")
	}
	if err := app.StartProxy("missing"); err == nil {
		t.Fatal("failed initialization did not block proxy start")
	}
	logs := app.GetLogs()
	if len(logs) == 0 || logs[0].Level != "ERROR" || !strings.Contains(logs[0].Message, "初始化配置服务失败") {
		t.Fatalf("startup configuration error not available to GUI: %+v", logs)
	}
	data, err := os.ReadFile(configPath)
	if err != nil || string(data) != "{invalid" {
		t.Fatalf("startup changed invalid configuration: %q, %v", data, err)
	}
}

func TestStartupReportsInvalidLogFile(t *testing.T) {
	app := newTestApp(t)
	settings := app.GetSettings()
	settings.LogFile = t.TempDir()
	if err := app.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}
	app = NewApp()
	app.startup(nil)
	defer app.shutdown(nil)
	for _, entry := range app.GetLogs() {
		if entry.Level == "ERROR" && strings.Contains(entry.Message, "日志文件") {
			return
		}
	}
	t.Fatal("configured log file could not be opened, but startup did not report the error")
}

func TestAppSettingsWriteErrors(t *testing.T) {
	for name, operation := range map[string]func(*App) error{
		"settings":         func(app *App) error { return app.UpdateSettings(service.AppSettings{}) },
		"auto start":       func(app *App) error { return app.SetAutoStart(true) },
		"minimize to tray": func(app *App) error { return app.SetMinimizeToTray(false) },
	} {
		t.Run(name, func(t *testing.T) {
			app := newTestApp(t)
			if err := os.Mkdir(filepath.Join(os.Getenv("HOME"), ".gotun", "config.json"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := operation(app); err == nil {
				t.Fatal("GUI reported success when settings could not be saved")
			}
		})
	}
}

func TestAddLogPreservesLevelAndMessage(t *testing.T) {
	app := NewApp()
	for _, level := range []string{"INFO", "ERROR", "DEBUG"} {
		app.AddLog(level, "message: "+level)
	}
	logs := app.GetLogs()
	if len(logs) != 3 {
		t.Fatalf("got %d log entries, want 3", len(logs))
	}
	for i, entry := range logs {
		wantLevel := []string{"INFO", "ERROR", "DEBUG"}[i]
		if entry.Level != wantLevel || entry.Message != "message: "+wantLevel || entry.ID != int64(i+1) {
			t.Fatalf("log arguments or sequence changed: %+v", entry)
		}
	}
}

func TestLogsRetentionIncrementalReadsAndSnapshots(t *testing.T) {
	app := NewApp()
	app.maxLogs = 3
	before := time.Now().Add(-time.Second)
	for i := 0; i < 5; i++ {
		app.AddLog("INFO", fmt.Sprintf("message-%d", i))
	}
	logs := app.GetLogs()
	if len(logs) != 3 || logs[0].ID != 3 || logs[2].ID != 5 || logs[0].Message != "message-2" {
		t.Fatalf("retention dropped the wrong logs: %+v", logs)
	}
	for _, entry := range logs {
		stamp, err := time.Parse(time.RFC3339, entry.Timestamp)
		if err != nil || stamp.Before(before) || stamp.After(time.Now()) {
			t.Fatalf("invalid timestamp: %+v, %v", entry, err)
		}
	}
	logs[0].Message = "local edit"
	incremental := app.GetLogsSince(3)
	if len(incremental) != 2 || incremental[0].ID != 4 || incremental[1].ID != 5 {
		t.Fatalf("incremental logs = %+v", incremental)
	}
	incremental[0].Message = "local edit"
	if got := app.GetLogs(); got[0].Message != "message-2" || got[1].Message != "message-3" {
		t.Fatalf("log snapshots share memory with app: %+v", got)
	}
	if got := app.GetLogsSince(5); len(got) != 0 {
		t.Fatalf("up-to-date cursor received logs: %+v", got)
	}
	app.ClearLogs()
	logs = app.GetLogsSince(5)
	if len(logs) != 1 || logs[0].ID != 6 || logs[0].Level != "INFO" || logs[0].Message != "日志已清空" {
		t.Fatalf("clearing logs reset cursor or lost clear notification: %+v", logs)
	}
}

func TestLogsExport(t *testing.T) {
	app := NewApp()
	app.AddLog("INFO", "启动连接")
	app.AddLog("ERROR", "connection failed")
	path := filepath.Join(t.TempDir(), "logs.txt")
	if err := os.WriteFile(path, []byte("old contents that should be replaced"), 0600); err != nil {
		t.Fatal(err)
	}
	before := app.GetLogs()
	if err := app.SaveLogsToFile(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var want strings.Builder
	for _, entry := range before {
		fmt.Fprintf(&want, "[%s] [%s] %s\n", entry.Timestamp, entry.Level, entry.Message)
	}
	if string(data) != want.String() {
		t.Fatalf("export = %q, want %q", data, want.String())
	}
	if !reflect.DeepEqual(app.GetLogs(), before) {
		t.Fatal("export changed retained logs")
	}
	if err := app.SaveLogsToFile(t.TempDir()); err == nil {
		t.Fatal("exporting over directory succeeded")
	}
	app = NewApp()
	if err := app.SaveLogsToFile(path); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || len(data) != 0 {
		t.Fatalf("empty log export = %q, %v", data, err)
	}
}

func TestLogConcurrentReadsWritesAndExports(t *testing.T) {
	app := NewApp()
	app.maxLogs = 2000
	var workers sync.WaitGroup
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for j := 0; j < 100; j++ {
				app.AddLog("INFO", "concurrent log")
				_ = app.GetLogs()
				_ = app.GetLogsSince(0)
			}
		}()
	}
	path := filepath.Join(t.TempDir(), "concurrent.log")
	for i := 0; i < 5; i++ {
		if err := app.SaveLogsToFile(path); err != nil {
			t.Fatal(err)
		}
	}
	workers.Wait()
	logs := app.GetLogs()
	if len(logs) != 800 {
		t.Fatalf("lost concurrent log entries: %d", len(logs))
	}
	for i, entry := range logs {
		if entry.ID != int64(i+1) {
			t.Fatalf("non-monotonic log IDs at %d: %+v", i, entry)
		}
	}
}
