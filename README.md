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
- Rule-based traffic splitting via configuration file
- Shell completion support for Bash, Zsh, Fish, PowerShell
- Structured logging and verbose mode for debugging

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
```

### Browser configuration

By default, gotun listens on `127.0.0.1:8080` (unless changed by `--listen`).

In your browser’s proxy settings:

- HTTP proxy host: `127.0.0.1`
- HTTP proxy port: `8080` (or the port you configured)

If system proxy support is enabled, some platforms can be configured automatically.

---

## Command-line options

| Flag              | Short | Description                                       | Default      |
|-------------------|-------|---------------------------------------------------|--------------|
| `--listen`        | `-l`  | Local HTTP proxy bind address                     | `:8080`      |
| `--port`          | `-p`  | SSH server port                                   | `22`         |
| `--pass`          |       | SSH password (not recommended; use interactively) |              |
| `--identity_file` | `-i`  | Private key file path                             |              |
| `--jump`          | `-J`  | Comma-separated jump hosts (`user@host:port`)     |              |
| `--target`        |       | Optional target network scope/coverage            |              |
| `--timeout`       |       | SSH connection timeout                            | `10s`        |
| `--verbose`       | `-v`  | Enable verbose logging                            | `false`      |
| `--log`           |       | Log file path                                     | stdout       |
| `--sys-proxy`     |       | Enable automatic system proxy configuration       | `true`       |
| `--rules`         |       | Path to routing rules configuration file          |              |

Run `gotun --help` for the full list of options.

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

## Rule-based routing (advanced)

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

Planned:

- [ ] RDP gateway support
- [ ] Tray/GUI frontend
- [ ] Export/import of configuration profiles
- [ ] SOCKS5 proxy support
- [ ] Connection pooling and performance tuning
- [ ] Traffic statistics and basic monitoring

---

## License

This project is licensed under the [MIT License](LICENSE).
