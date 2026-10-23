package main

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/Sesame2/gotun/internal/service"
	"github.com/Sesame2/gotun/internal/testutil"
)

func TestStopCancelsPendingSSHHandshake(t *testing.T) {
	app := newTestApp(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	host, port, _ := net.SplitHostPort(listener.Addr().String())
	if err := app.AddProfile(service.SSHProfile{ID: "silent", Host: host, Port: port, User: "audit", Password: "synthetic", HTTPAddr: "127.0.0.1:0"}); err != nil {
		t.Fatal(err)
	}
	started := make(chan error, 1)
	go func() { started <- app.StartProxy("silent") }()
	conn, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	stopped := make(chan error, 1)
	go func() { stopped <- app.StopProxy() }()
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		conn.Close()
		<-stopped
		t.Fatal("Stop waited for the SSH timeout instead of canceling startup")
	}
	if err := <-started; !errors.Is(err, context.Canceled) {
		t.Fatalf("start cancellation lost its cause: %v", err)
	}
}

func TestGUIRequiresExplicitTrustBeforeConnecting(t *testing.T) {
	app := newTestApp(t)
	server := testutil.NewSSHServer(t)
	host, port, _ := net.SplitHostPort(server.Addr)
	if err := app.AddProfile(service.SSHProfile{ID: "unknown", Host: host, Port: port, User: "tester", Password: "test-password", HTTPAddr: "127.0.0.1:0"}); err != nil {
		t.Fatal(err)
	}
	prompts := 0
	accept := false
	app.hostKeyPrompt = func(host, fingerprint string) bool {
		prompts++
		if host != server.Addr || !strings.HasPrefix(fingerprint, "SHA256:") {
			t.Errorf("missing host identity: %q %q", host, fingerprint)
		}
		return accept
	}
	if err := app.TestConnection("unknown"); err == nil || prompts != 1 {
		t.Fatalf("unknown host connected without approval: %v, prompts=%d", err, prompts)
	}
	accept = true
	if err := app.TestConnection("unknown"); err != nil {
		t.Fatal(err)
	}
	if err := app.TestConnection("unknown"); err != nil || prompts != 2 {
		t.Fatalf("approved identity was not remembered: %v, prompts=%d", err, prompts)
	}
}

func TestWindowsHostKeyDialogUsesNativeResponsesAndDefaultsToNo(t *testing.T) {
	for _, language := range []string{"zh-CN", "en-US"} {
		options := hostKeyDialogOptions("windows", language, "127.0.0.1:22", "SHA256:fixture")
		if options.Buttons[0] != "Yes" || options.DefaultButton != "No" || options.CancelButton != "No" {
			t.Fatalf("%s: Windows native Yes/No responses must match and default to rejection: %+v", language, options)
		}
	}
	options := hostKeyDialogOptions("darwin", "zh-CN", "127.0.0.1:22", "SHA256:fixture")
	if options.Buttons[0] != "信任" || options.DefaultButton != "取消" {
		t.Fatalf("macOS lost its localized explicit trust dialog: %+v", options)
	}
}
