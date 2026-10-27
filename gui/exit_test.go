package main

import (
	"errors"
	"testing"
)

func TestFailedNetworkCleanupKeepsAppOpenForRetry(t *testing.T) {
	app := newTestApp(t)
	app.exitCleanup = func() error { return errors.New("route restoration failed") }
	if err := app.QuitApp(); err == nil || app.quitting.Load() {
		t.Fatal("application quit despite failed network restoration")
	}
	app.exitCleanup = func() error { return nil }
	if err := app.QuitApp(); err != nil || !app.quitting.Load() {
		t.Fatalf("retry after restoring the network failed: %v", err)
	}
}

func TestWindowCloseCannotBypassFailedCleanup(t *testing.T) {
	app := newTestApp(t)
	app.exitCleanup = func() error { return errors.New("proxy restoration failed") }
	if !app.beforeClose(nil) || app.quitting.Load() {
		t.Fatal("window close bypassed failed network restoration")
	}
}
