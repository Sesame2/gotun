package utils

import "testing"

func TestReadPasswordFromTerminal(t *testing.T) {
	t.Skip("此测试需要人工交互，默认跳过")

	password, err := ReadPasswordFromTerminal("请输入测试密码：")
	if err != nil {
		t.Fatalf("读取密码出错：%v", err)
	}
	if password == "" {
		t.Error("读取的密码为空")
	}
}
