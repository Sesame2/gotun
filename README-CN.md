# gotun

[English](README.md) | 简体中文

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
- ✅ 支持 TUN 模式: 支持所有基于 TCP 协议的应用代理
- ✅ 自定义路由规则: 支持通过自定义的规则文件进行流量分流
- ✅ 命令行自动补全: 支持 Bash, Zsh, Fish, PowerShell

---

## 🚀 快速开始

### 安装

#### 使用安装脚本

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

# 开启 SOCKS5 代理 (默认 :1080)
gotun --socks5 :1080 user@example.com
```

> **注意**: 使用 SOCKS5 代理配合自定义路由规则时，建议在客户端（如浏览器或代理插件）中开启 "Proxy DNS when using SOCKS5" (远程DNS解析) 选项。否则客户端可能会在本地解析域名为 IP，导致基于域名的路由规则失效。

### 在浏览器中使用

启动代理后，在浏览器中配置HTTP代理：

- **代理地址**: `127.0.0.1`
- **端口**: `8080` (默认，或你指定的端口)

---

## 📖 操作手册

### 命令行参数

| 参数 | 简写 | 说明 | 默认值 |
|------|------|------|--------|
| `--http` | | 本地 HTTP 代理监听地址 (别名 `--listen`) | `:8080` |
| `--listen` | `-l` | [已废弃] 同 `--http` | `:8080` |
| `--socks5` | | SOCKS5 代理监听地址 | `:1080` |
| `--port` | `-p` | SSH 服务器端口 | `22` |
| `--pass` | | SSH 密码 (不安全, 建议使用交互式认证) | |
| `--identity_file` | `-i` | 用于认证的私钥文件路径 | |
| `--jump` | `-J` | 跳板机列表,用逗号分隔 (格式: user@host:port) | |
| `--http-upstream` | | 强制将所有 HTTP 请求转发到此上游 (格式: host:port) | |
| `--target` | | [已废弃] 同 `--http-upstream` | |
| `--timeout` | | 连接超时时间 | `10s` |
| `--verbose` | `-v` | 启用详细日志 | `false` |
| `--log` | | 日志文件路径 | 输出到标准输出 |
| `--sys-proxy` | | 自动设置/恢复系统代理 | `true` |
| `--rules` | | 代理规则配置文件路径 | |
| `--tun` | | 启用 TUN 模式 (VPN 模式) | `false` |
| `--tun-global` | `-g` | 启用全局 TUN 模式 (转发所有流量) | `false` |
| `--tun-ip` | | TUN 设备 CIDR 地址 | `10.0.0.1/24` |
| `--tun-route` | | 添加静态路由到 TUN (CIDR格式, 可多次使用) | |
| `--tun-nat` | | NAT 映射规则 (格式: LocalCIDR:RemoteCIDR) | |

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


### TUN 模式 (高级)

gotun 可以在本地创建一个虚拟网卡，将所有（或指定）TCP 流量拦截并通过 SSH 隧道透明传输。这使得不支持代理设置的软件也能通过 SSH 隧道访问远程资源。

#### 为什么使用 TUN 模式？

- **全应用代理**: 完美支持 **RDP 远程桌面**、**数据库连接** (MySQL/PostgreSQL)、**Redis** 等基于 TCP 的应用层协议。
- **无需配置**: 启用全局模式后，所有 TCP 流量自动走代理，无需在软件中逐个配置代理。
- **网络映射**: 可以将远程内网的整个网段映射到本地，解决本地与远程网段冲突的问题。

> **⚠️ 注意**: 当前版本的 TUN 模式仅支持 **TCP 协议**。不支持 UDP 流量和 ICMP 协议（因此无法使用 `ping` 命令测试连通性，请使用 `telnet` 或 `nc -vz` 测试 TCP 端口）。

#### 核心参数

| 参数 | 简写 | 说明 |
|------|------|------|
| `--tun` | | 显式启用 TUN 模式 (通常配合路由参数自动启用，可省略) |
| `--tun-global` | `-g` | **全局模式**：接管本机所有网络流量 (自动处理网关防止 SSH 断连) |
| `--tun-route` | | **指定网段代理**：仅将指定网段路由到 TUN (支持 CIDR，可多次使用) |
| `--tun-nat` | | **NAT 网段映射**：将本地网段映射到远程网段 (格式 `LocalCIDR:RemoteCIDR`) |
| `--tun-ip` | | 指定 TUN 设备的内部 IP (默认 `10.0.0.1/24`) |

#### 使用示例

**1. 全局模式**

将本机所有流量通过远程服务器转发。

> **⚠️ 警告**: 启动虚拟网卡可能会与 Clash、ZeroTier 等同样操作网卡或路由表的软件产生冲突。请谨慎使用全局 TUN 模式，建议优先使用指定网段代理模式。

```bash
# -g 自动启用 TUN 模式
sudo gotun -g user@server.com
```

**2. 指定网段代理**

仅将指定网段的流量放入隧道，其他流量直连。例如，只有访问 `10.0.0.0/24` 网段的流量才通过 SSH 隧道：

```bash
# 访问 10.0.0.x 的流量走 SSH，其他走本地
sudo gotun --tun-route 10.0.0.0/24 user@server.com
```

**3. NAT 网段映射**

解决网段冲突问题。例如：你需要访问的远程目标网段为 `192.168.0.0/24`，但你本地也有物理网卡或其他网络环境使用了 `192.168.0.0/24`。为了避免冲突，可以将远程的 `192.168.0.0/24` 映射到本地的一个无冲突网段（如 `10.0.0.0/24`）。

```bash
# 访问本地 10.0.0.1 -> 自动 NAT 到远程 192.168.0.1
sudo gotun --tun-nat 10.0.0.0/24:192.168.0.0/24 user@server.com
```

> **注意**: 
> - **权限**: TUN 模式需要 `sudo` (macOS/Linux) 或管理员权限 (Windows)。
> - **Windows 用户**: 首次运行时会自动释放 `wintun.dll`，无需手动安装驱动。

**4. RDP 远程桌面连接示例**

场景：你需要远程桌面连接到位于 `192.168.2.0/24` 网段的 Windows 机器（IP: `192.168.2.1`），但该网段无法直接访问。你有一台位于同一网段的 Linux 服务器（IP: `192.168.2.2`）开启了 SSH 服务。

```bash
# 将 192.168.2.0/24 网段的流量通过 SSH 隧道转发
sudo gotun --tun-route 192.168.2.0/24 user@192.168.2.2
```

启动后，你就可以直接打开 Windows 远程桌面客户端，输入 `192.168.2.1` 进行连接，就像你在同一个局域网内一样。


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
- [x] **SOCKS5 代理支持**: 更广泛的协议支持
- [x] **TUN 模式**: L3 级 VPN 支持 (全局/规则/NAT)
- [ ] **托盘 GUI 界面**: 图形化用户界面
- [ ] **配置文件导出/导入**: 配置管理功能
- [ ] **连接池优化**: 提升性能和稳定性
- [ ] **统计和监控**: 流量统计和连接监控

---