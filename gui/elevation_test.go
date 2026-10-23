package main

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

func TestElevationCancelKeepsCurrentInstance(t *testing.T) {
	app := newTestApp(t)
	app.launchElevated = func(string, []string) error { return errors.New("UAC canceled") }
	if err := app.RequestElevationRestart(); err == nil {
		t.Fatal("canceled authorization was reported as a successful restart")
	}
	if app.quitting.Load() {
		t.Fatal("canceling authorization quit the current instance")
	}
}

func TestElevationRestartRejectsDuplicateRequests(t *testing.T) {
	app := newTestApp(t)
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	app.launchElevated = func(string, []string) error {
		started <- struct{}{}
		<-release
		return errors.New("authorization canceled")
	}
	first := make(chan error, 1)
	second := make(chan error, 1)
	go func() { first <- app.RequestElevationRestart() }()
	<-started
	go func() { second <- app.RequestElevationRestart() }()
	select {
	case err := <-second:
		if err == nil {
			t.Error("duplicate request was accepted")
		}
	case <-time.After(time.Second):
		t.Error("duplicate request opened another authorization flow")
	}
	close(release)
	<-first
}

func TestElevationWaitsForNewInstanceReadiness(t *testing.T) {
	app := newTestApp(t)
	started := make(chan []string, 1)
	app.launchElevated = func(_ string, args []string) error { started <- args; return nil }
	result := make(chan error, 1)
	go func() { result <- app.RequestElevationRestart() }()
	args := <-started
	if app.quitting.Load() {
		t.Fatal("launcher success alone quit the current instance")
	}
	conn, err := net.Dial("tcp", args[1])
	if err != nil {
		t.Fatal(err)
	}
	_, _ = conn.Write([]byte(args[2] + "\n"))
	conn.Close()
	select {
	case err := <-result:
		if err != nil || !app.quitting.Load() {
			t.Fatalf("new instance ready but restart did not complete: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("new instance readiness was ignored")
	}
}

func TestElevationRejectsUnrelatedReadiness(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err := waitForElevatedInstance(ctx, t.TempDir(), func(args []string) error {
		conn, err := net.Dial("tcp", args[1])
		if err != nil {
			return err
		}
		_, _ = conn.Write([]byte("unrelated process\n"))
		return conn.Close()
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unrelated connection completed elevation: %v", err)
	}
}

func TestElevationCommandsQuotePathsAndPreserveConfigHome(t *testing.T) {
	for _, platform := range []string{"windows", "darwin", "linux"} {
		cmd, err := elevationCommand(platform, "/apps/O'Brien/GoTun", []string{"--gotun-elevation-handoff", "127.0.0.1:1234", "token", "/home/O'Brien"})
		if err != nil {
			t.Fatal(err)
		}
		joined := strings.Join(cmd.Args, " ")
		if !strings.Contains(joined, "--gotun-elevation-handoff") || !strings.Contains(joined, "127.0.0.1:1234") {
			t.Fatalf("%s lost handoff arguments: %v", platform, cmd.Args)
		}
		if platform == "windows" && !strings.Contains(joined, "O''Brien") {
			t.Fatal("PowerShell path is not quoted")
		}
	}
}
