# GUI 真实配置交互验收 Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** 使用用户指定的现有公司 SSH/跳板配置验证代理，逐个检查 GUI 控件与样式，并补充能复现问题的测试。

**Architecture:** 保持现有 Wails/React/MUI 结构。公司凭据只从用户已有配置读取，真实网络测试采用显式 opt-in 环境变量，不将凭据或带授权参数的 URL 写入仓库。原计划使用独立 app bundle 和临时配置；其 Computer Use 权限被拒绝后改用已授权的原生 GoTun，备份基线，删除/编辑/文件操作使用本轮测试数据，结束后恢复原设置和公司代理运行状态。样式修复沿用已有主题，涵盖暗/亮模式、最小窗口、键盘与可访问名称。

**Tech Stack:** macOS native CUA、Wails、React Testing Library/Vitest、Go testing、真实 SSH/HTTP CONNECT。

---

## Task 1: 基线与隔离

- [x] 只读识别截图对应公司配置，确认已有 HTTP 代理端口。
- [x] 原有代理请求验收 URL 返回 HTTP 200、标题 IDE（不记录授权查询参数或页面内容）。
- [x] 尝试独立测试实例；原生权限拒绝后停止该进程，记录限制并改用已授权 GoTun。
- [x] 记录浏览器页面渲染验证的权限限制，避免把 HTTP 200 当成页面可用性证明。

## Task 2: 控件与样式清单

- [x] Home：选择、启动/停止、测试连接、告警关闭、模式切换、TUN 权限弹窗取消。
- [x] Profiles：新增、编辑、取消/保存、连接测试、密码显隐、文件选择取消、跳板新增/移除、TUN 路由/NAT 新增/移除、删除确认/取消。
- [x] Logs：搜索、各级别、自动滚动、暂停/恢复、导出/取消、清空及错误反馈。
- [x] Settings：主题与语言各选项、详细日志、日志路径/选择取消、项目与反馈链接、通知关闭。
- [x] 最小 800×600 / 默认 1024×700 / 长文本与键盘焦点；暗/亮色对比、滚动/弹窗可达性。

## Task 3: 回归与修复

- [x] 为已复现的命名、标签、主题、布局/交互和状态问题先写失败测试。
- [x] 最小修复，不新增自启动/托盘等无关功能。
- [x] 增加可手动启用的真实公司配置端到端测试；默认 CI 无凭据时明确跳过。
- [x] 运行完整 React/Go/GUI/race 与本地 SSH/TLS 代理链路；构建后再次原生验证。真实公司页面仅保留权限拒绝前 HTTP 基线，最终公司 SSH 和代理启停成功；浏览器渲染受阻已明确记录。

## Task 4: 交付

- [x] `docs/GUI_INTERACTION_AUDIT.md` 保存控件逐项结果、样式问题及修复、真实网络结果、被阻止的验收范围。
- [x] 更新测试说明和最后验证结果，不保存公司凭据/授权 URL/敏感页面内容。
