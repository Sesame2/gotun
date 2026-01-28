package main

import (
	"testing"
	"time"
)

func TestNewApp(t *testing.T) {
	app := NewApp()
	if app == nil {
		t.Fatal("NewApp() returned nil")
	}
	if app.proxyService == nil {
		t.Error("proxyService should not be nil")
	}
	if app.logs == nil {
		t.Error("logs should not be nil")
	}
	if app.maxLogs != 1000 {
		t.Errorf("maxLogs should be 1000, got %d", app.maxLogs)
	}
}

func TestAddLog(t *testing.T) {
	app := NewApp()

	// 测试添加日志
	app.AddLog("INFO", "Test message 1")
	app.AddLog("ERROR", "Test message 2")
	app.AddLog("DEBUG", "Test message 3")

	logs := app.GetLogs()
	if len(logs) != 3 {
		t.Errorf("Expected 3 logs, got %d", len(logs))
	}

	// 验证日志内容
	if logs[0].Level != "INFO" {
		t.Errorf("Expected level INFO, got %s", logs[0].Level)
	}
	if logs[0].Message != "Test message 1" {
		t.Errorf("Expected message 'Test message 1', got '%s'", logs[0].Message)
	}

	// 验证日志 ID 递增
	if logs[0].ID >= logs[1].ID || logs[1].ID >= logs[2].ID {
		t.Error("Log IDs should be incrementing")
	}
}

func TestGetLogsSince(t *testing.T) {
	app := NewApp()

	app.AddLog("INFO", "Log 1")
	app.AddLog("INFO", "Log 2")
	app.AddLog("INFO", "Log 3")

	allLogs := app.GetLogs()
	lastID := allLogs[1].ID

	// 获取 ID 大于 lastID 的日志
	newLogs := app.GetLogsSince(lastID)
	if len(newLogs) != 1 {
		t.Errorf("Expected 1 new log, got %d", len(newLogs))
	}
	if newLogs[0].Message != "Log 3" {
		t.Errorf("Expected 'Log 3', got '%s'", newLogs[0].Message)
	}
}

func TestLogLimit(t *testing.T) {
	app := NewApp()
	app.maxLogs = 10 // 使用较小的限制进行测试

	// 添加超过限制数量的日志
	for i := 0; i < 15; i++ {
		app.AddLog("INFO", "Test log")
	}

	logs := app.GetLogs()
	if len(logs) > app.maxLogs {
		t.Errorf("Log count %d exceeds max %d", len(logs), app.maxLogs)
	}
}

func TestClearLogs(t *testing.T) {
	app := NewApp()

	app.AddLog("INFO", "Test 1")
	app.AddLog("INFO", "Test 2")

	// 清空日志（会添加一条 "日志已清空" 的日志）
	app.ClearLogs()

	logs := app.GetLogs()
	// ClearLogs 会添加一条日志
	if len(logs) != 1 {
		t.Errorf("Expected 1 log after clear, got %d", len(logs))
	}
}

func TestLogTimestamp(t *testing.T) {
	app := NewApp()

	before := time.Now().Add(-time.Second) // 给予 1 秒的缓冲
	app.AddLog("INFO", "Test")
	after := time.Now().Add(time.Second) // 给予 1 秒的缓冲

	logs := app.GetLogs()
	if len(logs) != 1 {
		t.Fatal("Expected 1 log")
	}

	logTime, err := time.Parse(time.RFC3339, logs[0].Timestamp)
	if err != nil {
		t.Fatalf("Failed to parse timestamp: %v", err)
	}

	if logTime.Before(before) || logTime.After(after) {
		t.Errorf("Log timestamp %v is out of expected range [%v, %v]", logTime, before, after)
	}
}

func TestLogConcurrency(t *testing.T) {
	app := NewApp()

	// 并发添加日志
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(n int) {
			for j := 0; j < 100; j++ {
				app.AddLog("INFO", "Concurrent test")
			}
			done <- true
		}(i)
	}

	// 等待所有 goroutine 完成
	for i := 0; i < 10; i++ {
		<-done
	}

	logs := app.GetLogs()
	if len(logs) != 1000 {
		t.Errorf("Expected 1000 logs, got %d", len(logs))
	}
}

func TestGetVersion(t *testing.T) {
	app := NewApp()

	version := app.GetVersion()
	if version == "" {
		t.Error("Version should not be empty")
	}
}
