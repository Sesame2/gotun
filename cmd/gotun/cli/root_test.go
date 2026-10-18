package cli

import (
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/Sesame2/gotun/internal/testutil"
)

// Each invocation gets Cobra's real argument parsing and a fresh global config.
func TestCLIProcess(t *testing.T) {
	if os.Getenv("GOTUN_CLI_TEST_PROCESS") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			rootCmd.SetArgs(os.Args[i+1:])
			break
		}
	}
	Execute("test-version")
	os.Exit(0)
}

func TestCLICommands(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
		fail bool
	}{
		{"help", []string{"--help"}, "gotun [user@host]", false},
		{"version", []string{"--version"}, "test-version", false},
		{"missing target", nil, "accepts 1 arg(s), received 0", true},
		{"extra targets", []string{"u@h", "u@h2"}, "accepts 1 arg(s), received 2", true},
		{"invalid target", []string{"missing-user"}, "无效的SSH目标格式", true},
		{"missing user", []string{"@host"}, "用户名或主机名不能为空", true},
		{"unknown flag", []string{"--nonexistent"}, "unknown flag", true},
		{"invalid timeout", []string{"--timeout", "bad", "u@h"}, "invalid duration", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{"-test.run=^TestCLIProcess$", "--"}, tc.args...)
			cmd := exec.Command(os.Args[0], args...)
			cmd.Env = append(os.Environ(), "GOTUN_CLI_TEST_PROCESS=1")
			output, err := cmd.CombinedOutput()
			if (err != nil) != tc.fail || !strings.Contains(string(output), tc.want) {
				t.Fatalf("CLI %v: err=%v, output=%s; want failure=%v containing %q", tc.args, err, output, tc.fail, tc.want)
			}
		})
	}
}

func TestParseSSHTarget(t *testing.T) {
	for _, tc := range []struct {
		target, user, host string
		fail               bool
	}{
		{"alice@example.com", "alice", "example.com", false},
		{"alice@host:2222", "alice", "host:2222", false},
		{"alice@[2001:db8::1]:22", "alice", "[2001:db8::1]:22", false},
		{"", "", "", false},
		{"host", "", "", true},
		{"@host", "", "", true},
		{"user@", "", "", true},
		{"a@b@c", "", "", true},
	} {
		t.Run(tc.target, func(t *testing.T) {
			user, host, err := parseSSHTarget(tc.target)
			if (err != nil) != tc.fail || user != tc.user || host != tc.host {
				t.Fatalf("parse = %q, %q, %v", user, host, err)
			}
		})
	}
}

func TestCLIFailedSOCKSSetupReleasesHTTPPort(t *testing.T) {
	server := testutil.NewSSHServer(t)
	previous := cfg
	cfg = server.Config()
	defer func() { cfg = previous }()
	cfg.ListenAddr = testutil.AvailableAddr(t)
	cfg.SocksAddr = "invalid-listen-address"
	if err := rootCmd.RunE(rootCmd, []string{"tester@" + server.Addr}); err == nil {
		t.Fatal("invalid SOCKS listener accepted")
	}
	listener, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		t.Fatalf("HTTP port remained bound after failed CLI setup: %v", err)
	}
	listener.Close()
}

func FuzzParseSSHTarget(f *testing.F) {
	for _, target := range []string{"", "user@host", "user@[::1]:22", "@", "a@b@c"} {
		f.Add(target)
	}
	f.Fuzz(func(t *testing.T, target string) {
		user, host, err := parseSSHTarget(target)
		if err == nil && target != "" && (user == "" || host == "" || user+"@"+host != target) {
			t.Fatalf("invalid successful parse of %q", target)
		}
	})
}
