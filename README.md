# gotun
English | [简体中文](README-CN.md)

[![Go Reference](https://pkg.go.dev/badge/github.com/Sesame2/gotun.svg)](https://pkg.go.dev/github.com/Sesame2/gotun)
[![Go Report Card](https://goreportcard.com/badge/github.com/Sesame2/gotun)](https://goreportcard.com/report/github.com/Sesame2/gotun)
[![Release](https://img.shields.io/github/v/release/Sesame2/gotun)](https://github.com/Sesame2/gotun/releases)
[![Downloads](https://img.shields.io/github/downloads/Sesame2/gotun/total.svg)](https://github.com/Sesame2/gotun/releases)
[![Homebrew](https://img.shields.io/homebrew/v/gotun)](https://formulae.brew.sh/formula/gotun)
![Go Version](https://img.shields.io/github/go-mod/go-version/Sesame2/gotun)
[![License](https://img.shields.io/github/license/Sesame2/gotun.svg)](LICENSE)

> Lightweight HTTP-over-SSH proxy written in Go, designed to be cross-platform and easy to deploy.


---

## Overview

`gotun` is a command-line HTTP-over-SSH proxy. It establishes an SSH connection to a remote host and uses that host as the **egress point** for HTTP(S) traffic. Local HTTP requests are forwarded through the SSH tunnel and executed from the remote host.

Typical use cases:

- Accessing routers, servers, APIs and other resources in a private network
- Reaching networks that are only visible from a specific host (bastion/jump host, corporate network, isolated segments, etc.)
- Using the remote host as an outbound HTTP proxy

---

## How it works

### Before: no direct access to internal resources

```text
Your machine                 Firewall/NAT                    Internal network
┌─────────┐                ┌─────────┐                     ┌─────────────┐
│         │   ❌ direct     │         │                     │  Router     │
│   PC    │ ─────────────▶ │  FW/NAT │                     │  NAS        │
│         │   blocked       │         │                     │  Servers    │
└─────────┘                └─────────┘                     └─────────────┘
```

### After: access via SSH bastion with gotun

```text
Your machine            SSH (tcp/22)                   Bastion                 Internal network
┌─────────┐          ┌─────────────┐                ┌─────────┐              ┌─────────────┐
│         │  HTTP    │  gotun HTTP │    SSH tunnel  │         │   internal   │  Router     │
│   PC    │ ◀──────▶ │   proxy     │◀──────────────▶│ Bastion │◀────────────▶│  NAS        │
│         │          └─────────────┘                │         │              │  Servers    │
└─────────┘                                         └─────────┘              └─────────────┘
   ↑
   └─ HTTP proxy set to 127.0.0.1:8080
```

### Why use gotun?

| Traditional approach                         | With gotun                                      |
|---------------------------------------------|-------------------------------------------------|
| Manual port forwards per service            | One SSH tunnel for all HTTP(S) traffic          |
| Multiple ports exposed on the bastion       | No extra open ports; uses existing SSH only     |
| Hard to manage multiple mappings            | Single proxy endpoint                           |
| Easy to accidentally expose internal hosts  | All traffic stays inside an encrypted SSH tunnel |

---

## Features

- No additional software required on the remote host (only SSH)
- All traffic is carried over an SSH tunnel
- Can reach any address that the remote host can reach (including internal addresses)
- Supports single and multi-hop SSH jump hosts
- Cross-platform: Windows, Linux, macOS
- Can be used as a system HTTP proxy (optional)
- TUN Mode: Supports standard TCP-based application proxying
- Rule-based traffic splitting via configuration file
- Shell completion support for Bash, Zsh, Fish, PowerShell
- Structured logging and verbose mode for debugging
- SOCKS5 proxy support

---

## Installation

### Install script

```bash
curl -fsSL https://raw.githubusercontent.com/Sesame2/gotun/main/scripts/install.sh | sh
```

The script installs `gotun` into `~/.local/bin` or `/usr/local/bin`.  
Make sure that directory is included in your `PATH`.

### Homebrew (macOS / Linux)

```bash
brew install gotun
```

### Download prebuilt binaries

Download the appropriate binary for your platform from the  
[Releases page](https://github.com/Sesame2/gotun/releases).

### Build from source

```bash
git clone https://github.com/Sesame2/gotun.git
cd gotun
make build
```

The binary will be placed under `build/`.

### `go install`

With Go 1.17+:

```bash
go install github.com/Sesame2/gotun/cmd/gotun@latest
```

> Note: when installing via `go install`, the `--version` output may not include an exact version number, as it depends on build-time metadata. For reproducible version information, prefer the install script, Homebrew, or release binaries.

Ensure `$GOBIN` or `$GOPATH/bin` is on your `PATH`.

---

## Quick start

### Basic usage

```bash
# Basic: connect to an SSH server and start an HTTP proxy
gotun user@example.com

# Use a non-default SSH port
gotun -p 2222 user@example.com

# Use a specific private key
gotun -i ~/.ssh/id_rsa user@example.com

# Change local proxy listen address/port
gotun --listen :8888 user@example.com

# Disable automatic system proxy configuration
gotun --sys-proxy=false user@example.com

# Enable SOCKS5 proxy (default listen on :1080)
gotun --socks5 :1080 user@example.com
```

> **Note**: When using SOCKS5 with custom routing rules, it is recommended to enable "Proxy DNS when using SOCKS5" (Remote DNS) in your client. Otherwise, the client might resolve domains to IPs locally, causing domain-based routing rules to fail.

### Browser configuration

By default, gotun listens on `127.0.0.1:8080` (unless changed by `--listen`).

In your browser’s proxy settings:

- HTTP proxy host: `127.0.0.1`
- HTTP proxy port: `8080` (or the port you configured)

If system proxy support is enabled, some platforms can be configured automatically.

---

## Command-line options

| Flag | Short | Description | Default |
|------|-------|-------------|---------|
| `--http` | | Local HTTP proxy listen address (alias for `--listen`) | `:8080` |
| `--listen` | `-l` | [Deprecated] Same as `--http` | `:8080` |
| `--socks5` | | SOCKS5 proxy listen address | `:1080` |
| `--port` | `-p` | SSH server port | `22` |
| `--pass` | | SSH password (insecure, interactive preferred) | |
| `--identity_file` | `-i` | Private key file path | |
| `--jump` | `-J` | Comma-separated jump hosts (`user@host:port`) | |
| `--http-upstream` | | Force forward all HTTP requests to this upstream (`host:port`) | |
| `--target` | | [Deprecated] Same as `--http-upstream` | |
| `--timeout` | | Connection timeout | `10s` |
| `--verbose` | `-v` | Enable verbose logging | `false` |
| `--log` | | Log file path | stdout |
| `--sys-proxy` | | Auto-configure system proxy | `true` |
| `--rules` | | Path to routing rules config file | |

---

## Typical scenarios

### 1. Accessing internal services through a bastion

Assume `jumpserver.company.com` can reach `192.168.1.100` inside a private network:

```bash
gotun admin@jumpserver.company.com
```

Once running and proxy is configured, you can browse:

- `http://192.168.1.100:8080`
- Other HTTP(S) endpoints reachable from the jumpserver

### 2. Development and debugging

You need to reach a remote dev environment (APIs, DB, etc.) from your local machine:

```bash
# Verbose logging and custom listen port
gotun --listen :8888 -v developer@dev-server.com
```

If you do not want gotun to modify system proxy settings:

```bash
gotun --sys-proxy=false --listen :8888 -v developer@dev-server.com
```

Then configure your IDE or tooling to use `127.0.0.1:8888` as HTTP proxy.

### 3. Using a remote host as internet egress

```bash
gotun user@proxy-server.com
```

All (or selected) HTTP(S) traffic will exit from the remote server’s network.

---

## Jump hosts

When the final target is only reachable via one or more intermediate hosts, you can configure jump hosts.


### Workflow diagram

```text
Your machine           SSH Tunnel            Jump Host             SSH Tunnel            Target Host
┌─────────┐         ┌───────────┐         ┌──────────┐         ┌───────────┐         ┌──────────┐
│         │   🔐    │           │    🔐   │          │    🔐   │           │   🌐   │          │
│   PC    │◀───────▶│ gotun tun │◀───────▶│ Bastion  │◀───────▶│ gotun tun │◀───────▶│  Target  │
│         │  HTTP   │ (encrypt) │         │          │         │ (encrypt) │        │          │
└─────────┘         └───────────┘         └──────────┘         └───────────┘         └──────────┘
```

### Single jump host

```bash
gotun -J user1@jump.host.com user2@target.server.com
```

Traffic flow:

```text
PC → SSH to jump.host.com → SSH to target.server.com → HTTP request
```

### Multiple jump hosts

```bash
gotun -J user1@jump1.com,user2@jump2.com user3@target.com
```

`gotun` will establish nested SSH tunnels through each hop in order.

---

## Authentication

### SSH key authentication (recommended)

```bash
# Explicit key
gotun -i ~/.ssh/id_rsa user@example.com

# Rely on default keys in ~/.ssh
gotun user@example.com
```

### Password authentication

```bash
# Interactive (recommended over CLI flags)
gotun user@example.com
# gotun will prompt for the password

# Non-interactive (not recommended)
gotun --pass 'yourpassword' user@example.com
```

Avoid passing passwords directly on the command line when possible.

---

## System proxy integration

By default (`--sys-proxy=true`), gotun attempts to:

1. Capture the current system proxy settings
2. Set the system HTTP proxy to its local listening address (e.g. `127.0.0.1:8080`)
3. Restore the original settings on exit

If you prefer to manage proxy settings yourself, run:

```bash
gotun --sys-proxy=false user@example.com
```

Platform notes:

- **macOS**: uses `networksetup`
- **Windows**: uses registry-based proxy configuration
- **Linux**: attempts to use desktop settings (e.g. GNOME) and/or environment variables where applicable

---

## Rule-based routing (Advanced)

`gotun` can read a Clash-style YAML rules file to decide which traffic is sent via the SSH proxy and which goes directly.

This is useful when you need:

- Direct access to local or corporate networks
- Proxy only for selected destinations (e.g. external services)

### Example rules file

`rules.yaml`:

```yaml
mode: rule

rules:
  # Direct access for internal domains and networks
  - DOMAIN-SUFFIX,internal.company.com,DIRECT
  - IP-CIDR,10.0.0.0/8,DIRECT
  - IP-CIDR,192.168.0.0/16,DIRECT
  - DOMAIN-SUFFIX,cn,DIRECT
  - DOMAIN-SUFFIX,qq.com,DIRECT

  # Specific domains via proxy
  - DOMAIN-SUFFIX,google.com,PROXY
  - DOMAIN-SUFFIX,github.com,PROXY

  # Everything else via proxy
  - MATCH,PROXY
```

Start gotun with the rules file:

```bash
gotun --rules ./rules.yaml user@your_ssh_server.com
```

Requests will be matched from top to bottom; the first matching rule applies.

---
## TUN Mode (Advanced)

gotun creates a local virtual network interface that intercepts specific (or all) TCP traffic and transparently tunnels it via SSH. This allows applications that don't support proxy settings to access remote resources through the SSH tunnel.

### Why use TUN Mode?

- **Full Application Proxy**: Perfectly supports **RDP (Remote Desktop)**, **Database connections** (MySQL/PostgreSQL), **Redis**, and other TCP-based application protocols.
- **Zero Config**: In Global Mode, all TCP traffic is routed automatically without per-app configuration.
- **Network Mapping**: Map a remote internal subnet to your local machine, solving IP conflict issues between local and remote networks.

> **⚠️ Note**: Current TUN Mode only supports **TCP protocol**. UDP traffic and ICMP (ping) are not supported (use `telnet` or `nc -vz` to test connectivity).

### Core Parameters

| Flag | Short | Description |
|------|-------|-------------|
| `--tun` | | Explicitly enable TUN mode (auto-enabled by other TUN flags, optional) |
| `--tun-global` | `-g` | **Global Mode**: Routes ALL network traffic (auto-handles gateway to prevent SSH drop) |
| `--tun-route` | | **Split Tunneling**: Route specific CIDRs to TUN (can be repeated) |
| `--tun-nat` | | **NAT Mapping**: Map local subnet to remote subnet (`LocalCIDR:RemoteCIDR`) |
| `--tun-ip` | | Internal IP for the TUN interface (default `10.0.0.1/24`) |

### Usage Examples

**1. Global Mode**

Route all local traffic through the remote server.

> **⚠️ Warning**: Global TUN mode might conflict with other software that modifies routing tables (e.g., Clash, ZeroTier). Use with caution or prefer Split Tunneling.

```bash
# -g automatically enables TUN mode
sudo gotun -g user@server.com
```

**2. Split Tunneling**

Route only specific subnets through the tunnel. For example, only traffic to `10.0.0.0/24` goes via SSH:

```bash
# Traffic to 10.0.0.x goes via SSH, everything else is direct
sudo gotun --tun-route 10.0.0.0/24 user@server.com
```

**3. NAT Mapping**

Solve subnet conflicts. For example, remote target is `192.168.0.0/24`, but your local network also uses this range. Map it to a conflict-free local range (e.g., `10.0.0.0/24`).

```bash
# Access Local 10.0.0.1 -> Auto-NAT -> Remote 192.168.0.1
sudo gotun --tun-nat 10.0.0.0/24:192.168.0.0/24 user@server.com
```

> **Note**: 
> - **Privileges**: TUN mode requires `sudo` (macOS/Linux) or Admin (Windows).
> - **Windows**: `wintun.dll` is auto-extracted on first run; no manual driver installation needed.

**4. RDP Remote Desktop Example**

Scenario: You need to RDP into a Windows machine at `192.168.2.1` (behind the SSH server), but you can't reach that IP directly. The SSH server (`192.168.2.2`) can reach it.

```bash
# Route traffic for 192.168.2.0/24 through the SSH tunnel
sudo gotun --tun-route 192.168.2.0/24 user@192.168.2.2
```

Once started, open your Remote Desktop Client and connect to `192.168.2.1` directly. It will feel like you are on the same LAN.

---
## Troubleshooting

### Connection issues

```bash
# Enable verbose logs
gotun -v user@example.com

# Log to a file
gotun -v --log ./gotun.log user@example.com
```

Check:

- SSH connectivity (`ssh user@example.com`)
- Firewall or security groups
- Correct key/port/jump configuration

### Permission issues (system proxy)

On some systems, changing system proxy settings requires elevated privileges:

```bash
# macOS / Linux
sudo gotun user@example.com

# Windows: run cmd/PowerShell as Administrator
gotun.exe user@example.com
```

If this is not acceptable, disable system proxy management and configure proxy settings manually:

```bash
gotun --sys-proxy=false user@example.com
```

---

## Status and roadmap

Implemented:

- [x] HTTP proxy
- [x] HTTPS tunneling via `CONNECT`
- [x] SSH key-based authentication
- [x] Interactive password authentication
- [x] Optional automatic system proxy configuration
- [x] Cross-platform support (Windows/Linux/macOS)
- [x] Verbose logging and log file output
- [x] CLI flags and subcommands
- [x] Single and multi-hop jump host support
- [x] Rule-based routing
- [x] Shell completion for common shells
- [x] SOCKS5 proxy support
- [x] TUN Mode: L3 VPN support (Global/Split/NAT)

Planned:

- [ ] Tray/GUI frontend
- [ ] Export/import of configuration profiles
- [ ] Connection pooling and performance tuning
- [ ] Traffic statistics and basic monitoring

---

## License

This project is licensed under the [MIT License](LICENSE).
