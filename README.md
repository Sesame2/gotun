# gotun

[![Go Reference](https://pkg.go.dev/badge/github.com/Sesame2/gotun.svg)](https://pkg.go.dev/github.com/Sesame2/gotun)
[![Go Report Card](https://goreportcard.com/badge/github.com/Sesame2/gotun)](https://goreportcard.com/report/github.com/Sesame2/gotun)
[![Release](https://img.shields.io/github/v/release/Sesame2/gotun)](https://github.com/Sesame2/gotun/releases)
[![Downloads](https://img.shields.io/github/downloads/Sesame2/gotun/total.svg)](https://github.com/Sesame2/gotun/releases)
[![Homebrew](https://img.shields.io/homebrew/v/gotun)](https://formulae.brew.sh/formula/gotun)
![Go Version](https://img.shields.io/github/go-mod/go-version/Sesame2/gotun)
[![License](https://img.shields.io/github/license/Sesame2/gotun.svg)](LICENSE)

> 🚀 基于 SSH 的轻量级 HTTP 代理工具，使用 Go 编写，跨平台开箱即用。


---

## ✨ 项目简介

`gotun` 是一个 HTTP-over-SSH 的命令行代理工具。它通过 SSH 协议连接远程主机，利用该主机作为 **网络出口**，将本地发起的 HTTP 请求安全地通过远程主机访问目标地址。你可以使用它来访问：

- 内网中的路由器、服务器、API 等资源
- 仅远程主机可访问的网络（公司内网、隔离网段等）
- 任意 HTTP 网站，使用远程主机作为「中转代理」

## 🎨 工作原理示意图

### 使用前：无法直接访问内网资源
```
你的电脑                    防火墙/NAT                    内网环境
┌─────────┐                ┌─────────┐                 ┌─────────────┐
│         │    ❌ 直接访问   │         │                 │  📱 路由器   │
│  💻 PC  │ ──────────────▶│  🔥🚫   │                 │  📺 NAS     │
│         │    被阻止/拒绝   │         │                 │  🖥️ 服务器   │
└─────────┘                └─────────┘                 │  📟 IoT设备  │
                                                       └─────────────┘
```

### 使用后：通过SSH跳板访问所有内网资源
```
你的电脑              SSH连接(22端口)           跳板机              内网环境
┌─────────┐          ┌─────────────┐         ┌─────────┐          ┌─────────────┐
│         │   🔐     │             │  🌐     │         │   ✅     │  📱 路由器   │
│  💻 PC  │◀────────▶│ gotun代理隧道│◀───────▶│ 🖥️ 跳板机 │◀───────▶│  📺 NAS     │
│         │  HTTP请求 │   (加密传输) │  SSH    │         │  内网访问 │  🖥️ 服务器   │
└─────────┘          └─────────────┘         └─────────┘          │  📟 IoT设备  │
    ↑                                                             └─────────────┘
    └── 浏览器设置代理: 127.0.0.1:8080
```

### 🔑 核心优势

| 传统方案 | gotun |
|---------|-----------|
| ❌ 需要复杂的端口转发配置 | ✅ 仅需一个SSH连接 |
| ❌ 每个服务需要单独映射端口 | ✅ 访问所有内网资源无需额外配置 |
| ❌ 容易暴露内网服务到公网 | ✅ 流量全程加密，安全可靠 |
| ❌ 管理多个连接复杂 | ✅ 一条隧道解决所有问题 |

**简单来说**: gotun 让你的电脑就像"坐在"跳板机旁边一样，可以访问跳板机能访问的任何资源！

---

## 🧱 项目特点

- ✅ 无需在远程主机部署任何代理服务
- ✅ 全部请求自动通过 SSH 加密隧道传输
- ✅ 支持访问任何远程主机可访问的地址（含内网）
- ✅ 支持多级跳板机 (Jump Host)，轻松穿透复杂网络
- ✅ 支持跨平台运行（Windows / Linux / macOS）
- ✅ 支持作为系统 HTTP 代理（可选扩展）
- ✅ 自定义路由规则: 支持通过自定义的规则文件进行流量分流
- ✅ 命令行自动补全: 支持 Bash, Zsh, Fish, PowerShell

---

## 🚀 快速开始

### 安装

#### 使用安装脚本 (推荐)

```bash
curl -fsSL https://raw.githubusercontent.com/Sesame2/gotun/main/scripts/install.sh | sh
```

脚本会将 `gotun` 安装到 `~/.local/bin` 或 `/usr/local/bin`。安装完成后，请确保安装目录已添加到您的 `PATH` 环境变量中。

#### 使用Homebrew安装 ( macOS / Linux )

```bash
brew install gotun
```

#### 下载预编译二进制文件

前往 [Releases](https://github.com/Sesame2/gotun/releases) 页面下载适合你系统的预编译版本。

#### 从源码编译

```bash
git clone https://github.com/Sesame2/gotun.git
cd gotun
make build
```

编译后的可执行文件位于 `build/` 目录下。

#### 使用 go install 安装

Go 1.17 及以上版本可直接通过以下命令安装：

```bash
go install github.com/Sesame2/gotun/cmd/gotun@latest
```

> **⚠️ 注意**: 使用 `go install` 安装的版本可能无法通过 `--version` 参数正确显示版本号，因为它直接从源码编译，缺少版本信息的注入。为了显示正确的版本信息，推荐使用安装脚本。

安装后，请确保你的 `$GOPATH/bin` 或 `$GOBIN` 目录已添加到系统 `PATH` 环境变量中。

### 基本使用

```bash
# 基本用法：连接到SSH服务器并启动系统代理
gotun user@example.com

# 指定SSH端口
gotun -p 2222 user@example.com

# 使用私钥认证
gotun -i ~/.ssh/id_rsa user@example.com

# 自定义代理监听端口
gotun --listen :8888 user@example.com

# 自动设置系统代理（默认开启）
# 若你希望启动时不修改系统代理，请显式关闭：
gotun --sys-proxy=false user@example.com
```

### 在浏览器中使用

启动代理后，在浏览器中配置HTTP代理：

- **代理地址**: `127.0.0.1`
- **端口**: `8080` (默认，或你指定的端口)

---

## 📖 操作手册

### 命令行参数

| 参数 | 简写 | 说明 | 默认值 |
|------|------|------|--------|
| `--listen` | `-l` | 本地HTTP代理监听地址 | `:8080` |
| `--port` | `-p` | SSH服务器端口 | `22` |
| `--pass` | | SSH密码 (不安全, 建议使用交互式认证) | |
| `--identity_file` | `-i` | 用于认证的私钥文件路径 | |
| `--jump` | `-J` | 跳板机列表,用逗号分隔 (格式: user@host:port) | |
| `--target` | | 可选的目标网络覆盖 | |
| `--timeout` | | 连接超时时间 | `10s` |
| `--verbose` | `-v` | 启用详细日志 | `false` |
| `--log` | | 日志文件路径 | 输出到标准输出 |
| `--sys-proxy` | | 自动设置/恢复系统代理 | `true` |
| `--rules` | | 代理规则配置文件路径 | |

### 使用场景

#### 1. 访问内网服务

假设 `jumpserver.company.com` 可以访问内网IP `192.168.1.100`。

```bash
# 连接到跳板机，启动代理服务
gotun admin@jumpserver.company.com
```

启动后，浏览器和其他支持系统代理的应用将自动通过 `gotun` 访问网络。现在可以直接在浏览器中打开 `http://192.168.1.100:8080` 等内网地址。

#### 2. 开发调试

在本地开发时，需要连接到远程开发服务器才能访问数据库或API。

```bash
# 启用详细日志进行调试，并指定监听端口
gotun --listen :8888 -v developer@dev-server.com
```

`gotun` 会自动设置系统代理（指向 `127.0.0.1:8888`）。开发工具如果支持系统代理，将能直接访问远程资源。如果不想影响系统其他应用的联网，可以禁用系统代理并手动配置开发工具：
`gotun --sys-proxy=false --listen :8888 -v developer@dev-server.com`

#### 3. 作为网络出口

将远程服务器作为你当前网络的出口，适合需要固定IP或访问特定网络资源的场景。

```bash
# 启动并自动配置为系统代理
gotun user@proxy-server.com
```

### 跳板机 (Jump Host)

当目标服务器无法直接访问，需要先登录一台或多台机器进行中转时，可以使用跳板机功能。

#### 工作原理示意图
```
你的电脑             SSH隧道             第一跳板机            SSH隧道             最终服务器
┌─────────┐         ┌───────────┐         ┌──────────┐         ┌───────────┐         ┌──────────┐
│         │   🔐    │           │    🔐   │          │    🔐   │           │   🌐   │          │
│  💻 PC  │◀───────▶│  gotun隧道 │◀───────▶│ 堡垒机/跳板机│◀───────▶│gotun隧道 │◀───────▶│ 目标主机 │
│         │  HTTP请求│ (加密)     │         │          │         │ (加密)     │        │          │
└─────────┘         └───────────┘         └──────────┘         └───────────┘         └──────────┘
```

#### 单级跳板机

通过 `jump.host.com` 连接到 `target.server.com`。

```bash
gotun -J user1@jump.host.com user2@target.server.com
```

#### 多级跳板机

通过 `jump1` -> `jump2` -> `target` 的顺序连接。

```bash
gotun -J user1@jump1.com,user2@jump2.com user3@target.com
```

`gotun` 会依次建立SSH隧道，最终连接到目标服务器。

### 认证方式

#### SSH私钥认证（推荐）

```bash
# 使用指定私钥文件
gotun -i ~/.ssh/id_rsa user@example.com

# 使用默认私钥（自动检测 ~/.ssh/ 目录下的密钥）
gotun user@example.com
```

#### 密码认证

```bash
# 交互式输入密码（推荐）
gotun user@example.com
# 程序会提示输入密码

# 命令行指定密码（不安全，不推荐）
gotun --pass yourpassword user@example.com
```

### 系统代理设置

默认情况下 (`--sys-proxy=true`)，`gotun` 会自动管理您操作系统的 HTTP 代理。如果您不希望 `gotun` 修改您的系统设置，可以在启动时使用 `--sys-proxy=false` 参数来禁用此功能。

当系统代理功能开启时，程序会：

1. **启动时**: 保存当前系统代理设置，然后设置为使用 gotun 代理
2. **运行中**: 所有系统网络流量通过 gotun 代理
3. **退出时**: 自动恢复原始的系统代理设置

支持的操作系统：

- ✅ **macOS**: 通过 `networksetup` 命令配置
- ✅ **Windows**: 通过注册表配置
- ✅ **Linux**: 通过 GNOME 设置和环境变量配置

### 自定义路由规则 (高级)

`gotun` 支持通过一个兼容 Clash 格式的 YAML 规则文件，来精细化地控制哪些网络请求通过 SSH 代理，哪些则直接连接。这对于希望同时访问内网资源（直连）和外部资源（代理）的场景非常有用。

#### 1. 创建规则文件

首先，创建一个规则文件，例如 `rules.yaml`：

```yaml
# rules.yaml
# 模式: rule (规则模式), global (全局代理), direct (全局直连)
mode: rule

# 规则列表 (从上到下匹配，第一个匹配的规则生效)
rules:
  # 规则：让公司内网和常用国内网站直连
  - DOMAIN-SUFFIX,internal.company.com,DIRECT
  - IP-CIDR,10.0.0.0/8,DIRECT
  - IP-CIDR,192.168.0.0/16,DIRECT
  - DOMAIN-SUFFIX,cn,DIRECT
  - DOMAIN-SUFFIX,qq.com,DIRECT

  # 规则：让特定服务走代理
  - DOMAIN-SUFFIX,google.com,PROXY
  - DOMAIN-SUFFIX,github.com,PROXY

  # 规则：所有其他未匹配的流量都走代理
  - MATCH,PROXY
```

#### 2. 启动 gotun

使用 `--rules` 参数指定规则文件的路径来启动 `gotun`。

```bash
gotun --rules ./rules.yaml user@your_ssh_server.com
```

现在，当您访问 `internal.company.com` 时，流量会直接发送；而访问 `google.com` 时，流量则会通过 SSH 隧道代理。


### 故障排除

#### 连接问题

```bash
# 启用详细日志进行调试
gotun -v user@example.com

# 指定日志文件
gotun -v --log ./gotun.log user@example.com
```

#### 权限问题

在某些系统上设置系统代理需要管理员权限：

```bash
# macOS/Linux
sudo gotun user@example.com

# Windows (以管理员身份运行 PowerShell/CMD)
.\gotun.exe user@example.com
```

---


## 🎯 功能状态

- [x] **HTTP 代理**: 完整的 HTTP 请求代理
- [x] **HTTPS 代理**: 支持 CONNECT 方法的 HTTPS 隧道
- [x] **SSH 私钥认证**: 支持多种私钥格式
- [x] **自动配置系统代理**: 跨平台系统代理设置
- [x] **交互式密码输入**: 安全的密码认证方式
- [x] **详细日志记录**: 支持调试和故障排除
- [x] **跨平台支持**: Windows/Linux/macOS
- [x] **命令行界面**: 完整的 CLI 参数支持
- [x] **跳板机 (Jump Host)**: 支持单级和多级SSH跳板机
- [x] **自定义路由规则**: 支持自定义的规则文件进行流量分流
- [x] **命令行自动补全**: 基于 Cobra 的智能提示
- [ ] **RDP网关**：支持RDP远程桌面网关
- [ ] **托盘 GUI 界面**: 图形化用户界面
- [ ] **配置文件导出/导入**: 配置管理功能
- [ ] **SOCKS5 代理支持**: 更广泛的协议支持
- [ ] **连接池优化**: 提升性能和稳定性
- [ ] **统计和监控**: 流量统计和连接监控

---