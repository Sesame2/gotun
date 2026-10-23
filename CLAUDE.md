# GoTun 开发指引

## 文档修改与提交确认

- 修改 `README*` 文件（含各级目录及语言版本，如 `README-CN.md`，不区分大小写）前，先展示文件路径、修改目的与拟议 diff，取得用户明确确认后再写入；新增、删除和重命名同样适用。
- 任何包含文档变更的 Git 提交前，先展示文档文件列表与最终 diff 摘要，取得用户明确确认后再执行。此规则包括 `CLAUDE.md`、README、`docs/` 和其他文档，也适用于与代码混合提交及 `commit --amend`。
- 确认仅覆盖已展示的版本、范围和操作；内容或范围变化后重新确认。修改授权不等于提交授权，提交授权不等于推送授权；同一版本、范围和操作已获明确确认时，不重复询问。

## 项目边界

- GoTun 提供基于 SSH 的 HTTP/SOCKS5 代理与 TUN 模式，同时包含 CLI 和 Wails 2 桌面 GUI。
- 根 Go 模块负责 `cmd/gotun/` 与 `internal/`；`gui/` 是独立 Go 模块，通过 `gui/go.mod` 的本地 `replace` 引用核心。根模块测试不会覆盖 GUI Go 模块或 React 前端。
- 桌面入口在 `gui/main.go`，前后端桥接在 `gui/app.go`，共享托盘行为在 `gui/tray.go`，平台适配在 `gui/internal/tray/`；React/TypeScript 前端在 `gui/frontend/`。
- 工具链版本以两个 `go.mod` 和 `.github/workflows/go.yml` 为准。GUI 开发还需要 Wails CLI、Node/npm 与目标系统的原生构建依赖。

## 常用命令

以下命令均从仓库根目录执行，优先复用现有 Makefile 目标。

| 目的 | 命令 |
| --- | --- |
| 构建 CLI | `make build` |
| 核心测试 / 竞态检测 | `make test` / `make test-race` |
| 核心静态检查 | `make vet` |
| React 测试，含绑定生成与依赖安装 | `make test-frontend` |
| GUI Go 测试，含前端构建与竞态检测 | `make test-gui` |
| 三层完整测试 | `make test-all` |
| GUI 开发 / 本机打包 | `make gui-dev` / `make gui-build` |

需要前端覆盖率及 GUI 静态检查时，按 CI 顺序执行：

```sh
make gui-bindings frontend-install
(cd gui/frontend && npm run test:coverage && npm run build)
(cd gui && go vet ./... && go test -race -count=1 -timeout=120s ./...)
```

- 核心 Go 检查明确使用 `./cmd/... ./internal/...`，避免扫描忽略的本地参考目录；不要用根目录 `go test ./...` 代替三层测试。
- 干净检出后，先生成 Wails 绑定再运行前端检查。`gui/main.go` 嵌入 `frontend/dist`，生成绑定时的占位资源必须由真实前端构建替换，才能用于发布。
- 测试命令以 [Makefile](Makefile)、[前端脚本](gui/frontend/package.json) 与 [CI](.github/workflows/go.yml) 为准；环境准备见 [GUI README](gui/README.md#development)。

## 修改约定

- CLI、GUI 与托盘共用的业务规则放在现有 `internal/` 服务中；平台相关行为保留在对应平台文件中，避免在 React 页面里复制代理生命周期逻辑。
- 修改暴露给前端的 Go 方法或类型后，运行 `make gui-bindings` 并检查调用方。`gui/frontend/wailsjs/` 是生成代码，不手工修改；构建产物、绑定、依赖目录和覆盖率报告保持忽略。
- 前端设置更新复用 `gui/frontend/src/settings.ts` 的 `saveSettingsPatch`，防止局部修改覆盖托盘或其他页面保存的设置。
- 新增前端文案同步维护 `gui/frontend/src/locales/zh-CN.ts` 与 `en-US.ts`，使用现有 `useI18n`；颜色和组件样式复用现有主题与样式文件。
- 修改 GUI 或托盘行为时，同时检查 macOS 与 Windows 的调用与实现。CI 在三平台检查核心，在 macOS/Windows 检查原生 GUI；不能将交叉编译通过当作实机验收。

## 测试边界

- SSH/代理集成测试复用 `internal/testutil/` 的本地 SSH fixture；配置写入使用临时 HOME/USERPROFILE，网络监听优先使用 `127.0.0.1:0`，不读取开发者真实账号或私钥。
- 前端测试保留真实生成的 Wails JS，在 `gui/frontend/src/test/bridge.ts` 的原生边界替换 `window.go` / `window.runtime`。异步交互根据请求完成或状态变化等待，避免依赖固定延时或放宽超时掩盖竞态。
- 普通自动化测试不启用 `GOTUN_LIVE_*`，不修改真实系统代理、路由或创建特权 TUN 设备。真实环境验证仅按当前任务的明确授权启用，入口见 [live_integration_test.go](internal/service/live_integration_test.go) 的 `TestLiveCompanyHTTPS`。
- GUI 样式变化除组件测试外，检查最小窗口、明暗主题和中英文布局；jsdom 不验证像素与原生托盘菜单，需单独记录实机验收结果。
- 根据改动选择对应层的检查；跨模块变更运行 `make test-all`。报告实际执行的命令、失败与未覆盖的平台，不用历史结果代替本次验证。

## 工作区与敏感信息

- 保留已有未提交文件和用户配置，提交时只暂存当前任务的文件。提交、推送、改写历史与修改分支保护按用户当前任务的授权执行。
- 密码、私钥、真实配置、带授权参数的 URL 不写入代码、文档、测试快照或日志；测试使用合成数据。
- 本文件只维护跨任务有效的项目约定，不加入临时任务状态、提交 SHA、覆盖率快照或个人机器路径。执行结果在任务回复、PR 或 CI 中记录，不额外生成流水账文档。
