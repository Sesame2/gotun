# GUI 分支分析与测试 Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** 分析本地 GUI 最新提交，运行 CLI/桌面应用，补充可重复的核心、服务层、GUI API 和前端测试，记录真实缺口。

**Architecture:** 保留现有 Go 核心 + 独立 Wails Go 模块 + React 前端结构。用临时目录隔离配置；用本地临时 SSH/HTTP/TCP 服务覆盖网络链路；仅修复测试复现的缺陷。涉及系统代理、管理员提权、真实 TUN 设备的操作列入人工验证，不在测试中修改用户系统。

**Tech Stack:** Go testing/race/coverage/fuzz、Wails v2、React/TypeScript、Vitest/Testing Library。

**验证结果：** 已完成所列自动化范围和 GUI 冒烟检查。根模块 92 个顶层测试/fuzz 入口通过、2 项跳过，GUI 15 个顶层测试通过，前端 54 个用例通过；Go 1.25.5/1.26.0 与 Node 20/25 验证通过；干净临时副本 `make test-all` 通过。明确的特权/平台限制记录在 `docs/TESTING.md`，剩余功能及风险在 `docs/PROJECT_ANALYSIS.md`。

---

## Task 1: 仓库和运行基线

- [x] 拉取所有远程分支和标签，保留 GUI 本地未推送提交。
- [x] 从本地 `gui` 创建 `codex/gui-tests`，保留忽略的构建产物和临时目录。
- [x] 运行根模块基线：`GOCACHE=/tmp/gotun-go-build go test -count=1 ./...`。
- [x] 生成 Wails JS 绑定并运行前端/GUI 构建，执行 CLI 与 GUI 启动检查。

## Task 2: 配置与 GUI API 测试

**Files:** `internal/service/config_service*_test.go`, `gui/app*_test.go`；复现后必要的 `internal/service/config_service.go`, `gui/app.go` 修复。

- [x] 覆盖配置 CRUD、重新加载、默认配置、结构化跳板机/TUN 转换、错误路径、数据隔离及敏感配置文件权限。
- [x] 覆盖 GUI 配置 API、未初始化/无效配置、日志增量/导出/并发；不实际提权或写用户目录。
- [x] 对真实缺陷先运行失败测试，再修复并验证。

## Task 3: 配置解析、路由与 TUN 纯逻辑

**Files:** `internal/config/config_test.go`, `internal/router/router_test.go`, `internal/tun/tun_test.go`；必要的对应源文件修复。

- [x] 用表驱动测试覆盖配置验证、跳板机解析、NAT CIDR、路由规则与域名边界。
- [x] 覆盖不需要管理员权限的 TUN 地址转换等逻辑，增加有意义的 fuzz 种子。
- [x] 先复现错误，再最小修复；记录需要特权的未验证范围。

## Task 4: 前端测试

**Files:** `gui/frontend/package.json`, `package-lock.json`, `vitest.config.ts`, `src/**/*.test.tsx` 和测试 setup；仅必要的前端缺陷修复。

- [x] 安装与现有 Vite/Node 兼容的测试工具，模拟 Wails 边界，不模拟组件内部逻辑。
- [x] 覆盖配置 CRUD、状态轮询及卸载、TUN 启停/错误、主题/语言持久化、主要页面交互。
- [x] 运行 `npm test` 与 `npm run build`。

## Task 5: 代理集成与可重复验证

**Files:** `internal/proxy/*_test.go`, `internal/service/proxy_service_test.go`, `cmd/gotun/cli/root_test.go`, `Makefile`, `.github/workflows/go.yml`。

- [x] 本地 SSH fixture 验证认证、跳板机、HTTP 转发/CONNECT、SOCKS5、拒绝规则和失败清理。
- [x] 覆盖 CLI 帮助/版本/参数错误和无特权系统代理地址解析。
- [x] 统一 CLI、GUI、前端测试入口，并在 CI 启用测试。
- [x] 执行完整测试、race、vet、覆盖率、构建；审查差异并记录各层结果。

## Task 6: 交付

- [x] `docs/PROJECT_ANALYSIS.md`：结构、分支状态、未完成项、缺陷、运行结果与测试边界。
- [x] `docs/TESTING.md`：从全新检出运行测试的命令、依赖和人工验证步骤。
- [x] 独立检查测试是否能捕获对应错误，修复审查发现并复跑相关验证。
