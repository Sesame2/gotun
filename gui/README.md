# gotun GUI
English | [简体中文](README-CN.md)

> Desktop client for gotun, designed to manage SSH proxy connections on macOS and Windows.

---

## Overview

`gotun GUI` provides a graphical interface for the same proxy core used by the command-line client. Save SSH profiles, start HTTP/SOCKS5 or TUN connections, and switch profiles from the menu bar or system tray.

Typical use cases:

- Accessing private networks through an SSH host or a chain of jump hosts
- Managing saved connections without repeating command-line options

For SSH transport and routing examples, see the [main README](../README.md).

---

## Features

- SSH profiles with password, private-key and multi-hop authentication
- HTTP/SOCKS5 proxy and TUN mode
- Optional system proxy configuration and external routing rules
- macOS menu bar and Windows system tray controls
- Connection status, logs and log export
- English and Simplified Chinese, with light, dark and system themes

---

## Installation

### Build from source

Requires Go (see [go.mod](go.mod)), Node.js 20/npm and Wails CLI v2.10.1. macOS builds need Xcode Command Line Tools; Windows needs WebView2. Run `wails doctor` to check the native build environment.

```bash
# From the repository root
go install github.com/wailsapp/wails/v2/cmd/wails@v2.10.1
make gui-build
```

The application will be placed under `gui/build/bin/`.

---

## Quick start

### Basic usage

1. Open the application and add a profile under **Profiles**.
2. Enter the SSH host, port, username and authentication details, then save and test the connection.
3. Select the profile and proxy mode on **Home**, then start the proxy.

### System proxy and tray

Use automatic system proxy configuration, or configure your applications to use the profile's HTTP/SOCKS5 listen address.

Select a profile from the menu bar or system tray to connect or switch connections. The menu also provides stop, clear system proxy, show/hide and quit actions.

> **Note**: Clearing the system proxy does not stop SSH forwarding. With **Minimize to Tray on Close** enabled, closing the window hides it; use **Quit GoTun** to exit. Launch at login and automatic connection on startup are not implemented.

### TUN mode

Enable TUN in the profile to route TCP applications through a virtual network interface. Global mode, specific routes and subnet mappings can be configured in the profile.

> **Note**: TUN requires administrator privileges and primarily handles IPv4 TCP traffic. General UDP and ICMP traffic are not supported. See [TUN mode](../README.md#tun-mode-advanced) for routing examples and limitations.

---

## Configuration

Profiles and application settings are stored in:

- macOS: `~/.gotun/config.json`
- Windows: `%USERPROFILE%\.gotun\config.json`

> **Note**: The configuration may contain plaintext passwords. Do not commit it to a repository or share it without removing credentials.

---

## Development

Run from the repository root:

```bash
# Generate bindings and install frontend dependencies
make gui-bindings frontend-install

# Start with hot reload
make gui-dev
```

---

## Testing

```bash
# From the repository root
make test-frontend  # React components and interactions
make test-gui       # GUI Go module
make test-all       # Core, frontend and GUI
```

Default tests use local fixtures and temporary configuration without changing the system proxy or creating a privileged TUN device. Native tray, file dialog and TUN behavior still require checks on the target OS.

See the [Makefile](../Makefile) and [CI workflow](../.github/workflows/go.yml) for test commands and platform coverage. Opt-in HTTPS checks are documented in the [test source](../internal/service/live_integration_test.go).

---

## License

This project is licensed under the [MIT License](../LICENSE).
