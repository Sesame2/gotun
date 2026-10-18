package utils

import (
	"strings"
	"testing"
)

func TestConfiguredSSHPasswordDoesNotPrompt(t *testing.T) {
	for _, interactive := range []bool{false, true} {
		password, err := GetSSHPassword("configured-secret", interactive, "user", "host")
		if err != nil || password != "configured-secret" {
			t.Fatalf("interactive=%v: password=%q err=%v", interactive, password, err)
		}
	}
}

func TestMissingNoninteractivePasswordDoesNotReadTerminal(t *testing.T) {
	_, err := GetSSHPassword("", false, "user", "host")
	if err == nil || strings.Contains(err.Error(), "读取密码") {
		t.Fatalf("noninteractive authentication attempted to read terminal: %v", err)
	}
}
