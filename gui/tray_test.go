package main

import (
	"github.com/Sesame2/gotun/gui/internal/tray"
	"github.com/Sesame2/gotun/internal/testutil"
	"net"
	"os"
	"strings"
	"testing"

	"github.com/Sesame2/gotun/internal/service"
)

func TestTrayMenuState(t *testing.T) {
	state := trayState{language: "zh-CN", profiles: []service.SSHProfile{{ID: "one", Name: "Alpha"}, {ID: "two", Name: "Beta"}}, activeID: "two", running: true, mode: "auto"}
	items := buildTrayMenu(state)
	if len(items) == 0 {
		t.Fatal("empty tray menu")
	}
	profiles := findTrayItem(items, "profiles")
	if profiles == nil || len(profiles.Children) != 2 || profiles.Children[0].Checked || !profiles.Children[1].Checked {
		t.Fatalf("wrong selected profile: %+v", profiles)
	}
	if !findTrayItem(items, "mode:auto").Checked || findTrayItem(items, "mode:manual").Checked {
		t.Fatal("wrong proxy mode")
	}
	if findTrayItem(items, "stop").Disabled {
		t.Fatal("running stop disabled")
	}
	state.busy = true
	for _, item := range buildTrayMenu(state) {
		if strings.HasPrefix(item.ID, "mode:") && !item.Disabled {
			t.Fatal("mode enabled during switching")
		}
	}
	state = trayState{language: "en-US"}
	items = buildTrayMenu(state)
	if !findTrayItem(items, "stop").Disabled || !findTrayItem(items, "profiles").Disabled {
		t.Fatal("idle actions enabled")
	}
	if !findTrayItem(items, "mode:follow-profile").Checked {
		t.Fatal("default profile preference missing")
	}
	for _, item := range items {
		for _, c := range item.Label {
			if c > 127 {
				t.Fatalf("untranslated English menu: %s", item.Label)
			}
		}
	}
}

func TestTrayClosePolicy(t *testing.T) {
	a := newTestApp(t)
	if a.shouldHideOnClose() {
		t.Fatal("window hidden before tray is ready")
	}
	a.trayReady.Store(true)
	if !a.shouldHideOnClose() {
		t.Fatal("default minimize setting ignored")
	}
	a.quitting.Store(true)
	if a.shouldHideOnClose() {
		t.Fatal("explicit quit hides instead of exiting")
	}
	a.quitting.Store(false)
	if err := a.SetMinimizeToTray(false); err != nil {
		t.Fatal(err)
	}
	if a.shouldHideOnClose() {
		t.Fatal("disabled minimize setting ignored")
	}
}

func TestTrayInvalidSelectionPreservesRunningProxy(t *testing.T) {
	a := newTestApp(t)
	p := addLocalTrayProfile(t, a, "one")
	if err := a.StartProxy(p.ID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.StopProxy() })
	if err := a.selectTrayProfile("missing"); err == nil {
		t.Fatal("missing profile accepted")
	}
	if !a.proxyService.IsRunning() || a.GetProxyStatus().ProfileID != p.ID {
		t.Fatal("invalid selection stopped current connection")
	}
	if err := a.selectTrayProfile(p.ID); err != nil {
		t.Fatal(err)
	}
	if a.GetProxyStatus().ProfileID != p.ID {
		t.Fatal("same selection lost active profile")
	}
}

func addLocalTrayProfile(t *testing.T, a *App, id string) service.SSHProfile {
	t.Helper()
	server := testutil.NewSSHServer(t)
	testutil.TrustKnownHosts(t, os.Getenv("HOME"), server)
	cfg := server.Config()
	host, port, err := net.SplitHostPort(cfg.SSHServer)
	if err != nil {
		t.Fatal(err)
	}
	p := service.SSHProfile{ID: id, Name: id, Host: host, Port: port, User: cfg.SSHUser, Password: cfg.SSHPassword, HTTPAddr: testutil.AvailableAddr(t), SystemProxy: false}
	if err := a.AddProfile(p); err != nil {
		t.Fatal(err)
	}
	return p
}

func findTrayItem(items []tray.Item, id string) *tray.Item {
	for i := range items {
		if items[i].ID == id {
			return &items[i]
		}
		if found := findTrayItem(items[i].Children, id); found != nil {
			return found
		}
	}
	return nil
}

func TestTraySwitchingProfilesAndFailure(t *testing.T) {
	a := newTestApp(t)
	one := addLocalTrayProfile(t, a, "first")
	two := addLocalTrayProfile(t, a, "second")
	t.Cleanup(func() { a.stopAllProxies() })
	for _, id := range []string{one.ID, two.ID, one.ID} {
		if err := a.selectTrayProfile(id); err != nil {
			t.Fatal(err)
		}
		if got := a.GetProxyStatus(); got.Status != service.StatusRunning || got.ProfileID != id {
			t.Fatalf("active status: %+v", got)
		}
	}
	bad := service.SSHProfile{ID: "bad", Name: "bad", Host: "127.0.0.1", Port: "0", User: "tester", Password: "test", HTTPAddr: "127.0.0.1:0"}
	if err := a.AddProfile(bad); err != nil {
		t.Fatal(err)
	}
	if err := a.selectTrayProfile(bad.ID); err == nil {
		t.Fatal("invalid port accepted")
	}
	if !a.proxyService.IsRunning() {
		t.Fatal("validation failure stopped active connection")
	}
	if err := a.stopAllProxies(); err != nil {
		t.Fatal(err)
	}
	if a.traySnapshot().running {
		t.Fatal("stop still appears running")
	}
}

func TestTrayModePersistenceAndStaleFrontendSettings(t *testing.T) {
	a := newTestApp(t)
	stale := a.GetSettings()
	if err := a.setSystemProxyMode("auto"); err != nil {
		t.Fatal(err)
	}
	if a.GetSettings().SystemProxyMode != "auto" {
		t.Fatal("mode not persisted")
	}
	stale.Theme = "light"
	if err := a.UpdateSettings(stale); err != nil {
		t.Fatal(err)
	}
	if a.GetSettings().SystemProxyMode != "auto" || a.GetSettings().Theme != "light" {
		t.Fatal("stale GUI update overwrote tray preference")
	}
	if err := a.setSystemProxyMode("invalid"); err == nil {
		t.Fatal("invalid mode accepted")
	}
	if a.GetSettings().SystemProxyMode != "auto" {
		t.Fatal("invalid mode changed preference")
	}
	if err := a.setSystemProxyMode("manual"); err != nil {
		t.Fatal(err)
	}
	if a.GetSettings().SystemProxyMode != "manual" {
		t.Fatal("manual mode not persisted")
	}
}

func TestTrayDoesNotConnectWhileQuitting(t *testing.T) {
	a := newTestApp(t)
	p := addLocalTrayProfile(t, a, "profile")
	a.quitting.Store(true)
	if err := a.selectTrayProfile(p.ID); err == nil {
		t.Fatal("started a connection after quit")
	}
	if a.proxyService.IsRunning() {
		t.Fatal("proxy running after quit")
	}
}

func TestApplicationMenuQuitBypassesMinimize(t *testing.T) {
	a := newTestApp(t)
	a.trayReady.Store(true)
	menu := a.applicationMenu()
	entries := menu.Items[0].SubMenu.Items
	quit := entries[len(entries)-1]
	if quit.Accelerator == nil {
		t.Fatal("quit keyboard shortcut missing")
	}
	quit.Click(nil)
	if !a.quitting.Load() || a.shouldHideOnClose() {
		t.Fatal("native menu quit hides window")
	}
}
