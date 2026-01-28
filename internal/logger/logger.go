package logger

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
)

// LogLevel 定义日志级别
type LogLevel int

const (
	LevelDebug LogLevel = iota
	LevelInfo
	LevelWarn
	LevelError
	LevelFatal
)

// LogCallback 日志回调函数类型
type LogCallback func(level string, message string)

// Logger 处理应用日志
type Logger struct {
	verbose  bool
	logger   *log.Logger
	level    LogLevel
	file     *os.File
	callback LogCallback
}

// NewLogger 创建日志记录器
func NewLogger(verbose bool) *Logger {
	return &Logger{
		verbose: verbose,
		logger:  log.New(os.Stdout, "", log.LstdFlags),
		level:   LevelInfo,
	}
}

// SetCallback 设置日志回调函数
func (l *Logger) SetCallback(cb LogCallback) {
	l.callback = cb
}

// GetCallback 获取日志回调函数
func (l *Logger) GetCallback() LogCallback {
	return l.callback
}

// SetLogFile 设置日志输出文件
func (l *Logger) SetLogFile(filePath string) error {
	if filePath == "" {
		return nil
	}

	// 确保日志目录存在
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("无法创建日志目录: %w", err)
	}

	// 打开或创建日志文件
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("无法打开日志文件: %w", err)
	}

	// 关闭之前的日志文件
	if l.file != nil {
		l.file.Close()
	}

	l.file = file

	// 同时输出到控制台和文件
	writer := io.MultiWriter(os.Stdout, file)
	l.logger.SetOutput(writer)

	return nil
}

// SetLevel 设置日志级别
func (l *Logger) SetLevel(level LogLevel) {
	l.level = level
}

// 日志输出方法
func (l *Logger) log(level LogLevel, format string, args ...interface{}) {
	if level < l.level {
		return
	}

	var prefix string
	var levelStr string

	switch level {
	case LevelDebug:
		prefix = "[DEBUG] "
		levelStr = "DEBUG"
	case LevelInfo:
		prefix = "[INFO] "
		levelStr = "INFO"
	case LevelWarn:
		prefix = "[WARN] "
		levelStr = "WARN"
	case LevelError:
		prefix = "[ERROR] "
		levelStr = "ERROR"
	case LevelFatal:
		prefix = "[FATAL] "
		levelStr = "ERROR"
	}

	msg := fmt.Sprintf(format, args...)
	l.logger.Println(prefix + msg)

	// 调用回调
	if l.callback != nil {
		l.callback(levelStr, msg)
	}
}

// Debug 记录调试日志
func (l *Logger) Debug(msg string) {
	if l.verbose {
		l.log(LevelDebug, "%s", msg)
	}
}

// Debugf 记录格式化调试日志
func (l *Logger) Debugf(format string, args ...interface{}) {
	if l.verbose {
		l.log(LevelDebug, format, args...)
	}
}

// Info 记录信息日志
func (l *Logger) Info(msg string) {
	l.log(LevelInfo, "%s", msg)
}

// Infof 记录格式化信息日志
func (l *Logger) Infof(format string, args ...interface{}) {
	l.log(LevelInfo, format, args...)
}

// Warn 记录警告日志
func (l *Logger) Warn(msg string) {
	l.log(LevelWarn, "%s", msg)
}

// Warnf 记录格式化警告日志
func (l *Logger) Warnf(format string, args ...interface{}) {
	l.log(LevelWarn, format, args...)
}

// Error 记录错误日志
func (l *Logger) Error(msg string) {
	l.log(LevelError, "%s", msg)
}

// Errorf 记录格式化错误日志
func (l *Logger) Errorf(format string, args ...interface{}) {
	l.log(LevelError, format, args...)
}

// Fatal 记录致命错误并退出
func (l *Logger) Fatal(msg string) {
	l.log(LevelFatal, "%s", msg)
	os.Exit(1)
}

// Fatalf 记录格式化致命错误并退出
func (l *Logger) Fatalf(format string, args ...interface{}) {
	l.log(LevelFatal, format, args...)
	os.Exit(1)
}

// Close 关闭日志系统
func (l *Logger) Close() error {
	if l.file != nil {
		return l.file.Close()
	}
	return nil
}
