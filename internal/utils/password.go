package utils

import (
	"fmt"
	"syscall"

	"golang.org/x/term"
)

// 从终端读取密码不回显
func ReadPasswordFromTerminal(prompt string) (string, error) {
	fmt.Print(prompt)

	// 禁用回显
	passwordByte, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println() // 换行
	if err != nil {
		return "", fmt.Errorf("读取密码失败： %v", err)
	}

	return string(passwordByte), nil
}

// 获取SSH密码，优先使用配置中的密码，否则则交互式获取密码
func GetSSHPassword(configPassword string, interactive bool, user, server string) (string, error) {
	if configPassword != "" && !interactive {
		return configPassword, nil
	}

	// 交互式获取密码
	prompt := fmt.Sprintf("请输入%s@%s的密码：", user, server)
	return ReadPasswordFromTerminal(prompt)
}
