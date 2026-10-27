# gotun GUI

[English](README.md) | 简体中文

> gotun 的桌面客户端，用于在 macOS 和 Windows 上管理 SSH 代理连接。

---

## ✨ 项目简介

`gotun GUI` 与命令行客户端使用相同的代理核心。你可以保存 SSH 配置，启动 HTTP/SOCKS5 或 TUN 连接，并从菜单栏或系统托盘切换配置。

常见用途：

- 通过 SSH 主机或多级跳板机访问内网
- 管理已保存的连接，无需重复输入命令行参数

SSH 传输与路由示例见[项目说明](../README-CN.md)。

---

## 🧱 项目特点

- SSH 配置管理，支持密码、私钥和多级跳板机认证
- HTTP/SOCKS5 代理与 TUN 模式
- 可选系统代理设置与外部路由规则
- macOS 菜单栏与 Windows 系统托盘控制
- 连接状态、日志查看与导出
- 简体中文和英文，浅色、深色与跟随系统主题

---

## 安装

### 从源码构建

需要 Go（版本见 [go.mod](go.mod)）、Node.js 20/npm 和 Wails CLI v2.10.1。macOS 构建需要 Xcode Command Line Tools，Windows 需要 WebView2。可用 `wails doctor` 检查本机原生构建环境。

```bash
# 在项目根目录执行
go install github.com/wailsapp/wails/v2/cmd/wails@v2.10.1
make gui-build
```

应用产物位于 `gui/build/bin/`。

---

## 🚀 快速开始

### 基本使用

1. 打开应用，在「配置管理」中添加配置。
2. 填写 SSH 主机、端口、用户名和认证信息，保存并测试连接。
3. 在「首页」选择配置和代理模式，启动代理。

### 系统代理与托盘

可以使用自动系统代理配置，也可以让浏览器等客户端使用该配置的 HTTP/SOCKS5 监听地址。

在菜单栏或系统托盘中选择配置即可连接或切换。菜单还提供停止代理、清除系统代理、显示或隐藏窗口及退出操作。

> **注意**：清除系统代理不会停止 SSH 转发。启用「关闭时最小化到托盘」后，关闭窗口会隐藏主界面；请通过「退出 GoTun」结束应用。开机自启动和启动自动连接尚未实现。

### TUN 模式

在配置中启用 TUN，可通过虚拟网卡转发 TCP 应用流量。配置中支持全局模式、指定路由和子网映射。

> **注意**：TUN 需要管理员权限，主要处理 IPv4 TCP 流量，不支持通用 UDP 和 ICMP。路由示例和限制见 [TUN 模式](../README-CN.md#tun-模式-高级)。

---

## 配置文件

SSH 配置与应用设置保存在：

- macOS：`~/.gotun/config.json`
- Windows：`%USERPROFILE%\.gotun\config.json`

> **注意**：配置文件可能包含明文密码。请勿提交到仓库，分享前应移除凭据。

---

## 开发环境

在项目根目录执行：

```bash
# 生成绑定并安装前端依赖
make gui-bindings frontend-install

# 启动热重载开发模式
make gui-dev
```

---

## 测试

```bash
# 在项目根目录执行
make test-frontend  # React 组件与交互
make test-gui       # GUI Go 模块
make test-all       # 核心、前端与 GUI
```

默认测试使用本地 fixture 和临时配置，不修改系统代理或创建特权 TUN 设备。原生托盘、文件对话框及 TUN 行为仍需在目标系统验证。

测试命令与平台覆盖范围见 [Makefile](../Makefile) 和 [CI 工作流](../.github/workflows/go.yml)。显式启用的 HTTPS 检查见[测试源码](../internal/service/live_integration_test.go)。

---

## 许可证

本项目采用 [MIT 许可证](../LICENSE)。
