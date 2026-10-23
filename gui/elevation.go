package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const elevationFlag = "--gotun-elevation-handoff"

func waitForElevatedInstance(ctx context.Context, home string, launch func([]string) error) error {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer listener.Close()
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return err
	}
	token := hex.EncodeToString(secret)
	ready := make(chan struct{}, 1)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
			value, _ := bufio.NewReaderSize(conn, 128).ReadSlice('\n')
			conn.Close()
			if string(value) == token+"\n" {
				ready <- struct{}{}
				return
			}
		}
	}()
	launched := make(chan error, 1)
	go func() { launched <- launch([]string{elevationFlag, listener.Addr().String(), token, home}) }()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("新实例尚未就绪，保留当前实例: %w", ctx.Err())
		case err := <-launched:
			if err != nil {
				return fmt.Errorf("提权失败，保留当前实例: %w", err)
			}
			launched = nil
		case <-ready:
			return nil
		}
	}
}

func elevationCommand(platform, executable string, args []string) (*exec.Cmd, error) {
	quoteShell := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
	quotePS := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }
	switch platform {
	case "windows":
		// Start-Process joins ArgumentList; quote the native command line as well as PowerShell.
		var nativeArgs []string
		for _, arg := range args {
			nativeArgs = append(nativeArgs, quotePS(windowsCommandLineArg(arg)))
		}
		return exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command",
			"$ErrorActionPreference='Stop'; Start-Process -FilePath "+quotePS(executable)+" -ArgumentList @("+strings.Join(nativeArgs, ",")+") -Verb RunAs"), nil
	case "darwin":
		command := quoteShell(executable)
		for _, arg := range args {
			command += " " + quoteShell(arg)
		}
		return exec.Command("osascript", "-e", "do shell script "+strconv.Quote("("+command+" </dev/null >/dev/null 2>&1 &) ")+" with administrator privileges"), nil
	case "linux":
		if _, err := exec.LookPath("pkexec"); err == nil {
			return exec.Command("pkexec", append([]string{executable}, args...)...), nil
		}
		return exec.Command("xterm", append([]string{"-e", "sudo", executable}, args...)...), nil
	default:
		return nil, fmt.Errorf("不支持的操作系统: %s", platform)
	}
}

func windowsCommandLineArg(arg string) string {
	if arg != "" && !strings.ContainsAny(arg, " \t\n\v\"") {
		return arg
	}
	var result strings.Builder
	result.WriteByte('"')
	slashes := 0
	for _, char := range arg {
		if char == '\\' {
			slashes++
			continue
		}
		if char == '"' {
			result.WriteString(strings.Repeat("\\", slashes*2+1))
		} else {
			result.WriteString(strings.Repeat("\\", slashes))
		}
		slashes = 0
		result.WriteRune(char)
	}
	result.WriteString(strings.Repeat("\\", slashes*2))
	result.WriteByte('"')
	return result.String()
}

func launchElevated(executable string, args []string) error {
	cmd, err := elevationCommand(runtime.GOOS, executable, args)
	if err != nil {
		return err
	}
	return cmd.Run()
}

func elevationArguments() []string {
	if len(os.Args) == 5 && os.Args[1] == elevationFlag {
		return os.Args[2:]
	}
	return nil
}

func prepareElevationHome() {
	if args := elevationArguments(); args != nil {
		_ = os.Setenv("HOME", args[2])
		_ = os.Setenv("USERPROFILE", args[2])
	}
}

func (a *App) acknowledgeElevation() {
	args := elevationArguments()
	if args == nil || !a.IsRunningAsAdmin() || a.configService == nil {
		return
	}
	conn, err := net.DialTimeout("tcp", args[0], 2*time.Second)
	if err != nil {
		a.AddLog("ERROR", "无法确认提权重启，原实例将继续运行")
		return
	}
	defer conn.Close()
	_ = conn.SetWriteDeadline(time.Now().Add(time.Second))
	_, _ = fmt.Fprintln(conn, args[1])
}
