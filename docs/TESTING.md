# 测试与运行

## 从干净源码开始

需要 Go 1.25.5 或以上（以根目录 `go.mod` 为准）、Node.js 20/npm、Wails 2.10.1；macOS GUI 构建需要 Xcode Command Line Tools。Wails CLI 版本固定，避免无意升级框架。另已验证 Go 1.26.0 与 Node 25.6.1 的测试兼容性。

```sh
go install github.com/wailsapp/wails/v2/cmd/wails@v2.10.1
make test-all
make gui-build
```

`make test-all` 依次运行根模块测试，生成 Wails 绑定，按 package-lock 安装前端依赖，运行前端测试，构建真实前端，最后测试独立 GUI Go 模块。生成绑定前创建忽略的 dist 占位文件，解除 `go:embed` 与绑定生成的循环依赖；它不是最终应用资源，随后由 Vite 构建替换。

不要用根目录 `go test ./...` 代替三层测试：Go 不会跨入 `gui/go.mod`，也不会运行 React 测试。测试入口明确排除仓库中临时参考目录。

## 常用命令

```sh
make test                 # 根模块
make test-race            # 根模块 + 竞态检测
make test-coverage        # build/coverage.out，附函数覆盖率
make test-frontend        # 生成绑定、安装依赖、React 测试
make test-gui             # 构建前端、独立 GUI 模块 + race
make vet

cd gui/frontend
npm run test:watch
npm run test:coverage     # coverage/coverage-summary.json
npm run build
```

已经安装依赖/生成绑定时，可直接运行：

```sh
go test -race -count=1 -timeout=120s ./cmd/... ./internal/...
cd gui
go test -race -count=1 -timeout=120s ./...
go vet ./...
```

沙箱中 Go 默认缓存可能不可写，可临时添加 `GOCACHE=/tmp/gotun-go-build`；网络集成测试需要允许本机 loopback 监听。不要把 bind 权限错误当成代码回归。Go/npm 的依赖下载与 macOS 原生构建也需要对应缓存/网络访问。

## 测试覆盖什么

| 层 | 测试内容 |
| --- | --- |
| CLI | 帮助/版本/参数错误真实子进程、SSH 目标解析、初始化失败资源清理。 |
| 配置/路由 | 默认配置、认证/跳板验证、端口边界、NAT CIDR、YAML/规则顺序、模式、域名边界/IPv4/IPv6、fuzz 种子。 |
| SSH/代理 | 本机生成 Ed25519 密钥和 SSH 服务，密码/密钥/旧新跳板，真实 direct-tcpip、远端域名解析、HTTP POST/CONNECT、SOCKS、拒绝规则、认证协商、报错。 |
| 生命周期 | 端口占用、启停/重启、被拒绝的第二配置、旧规则清理、SetConfig/Start/Stop 并发、既有隧道与半关闭、关闭时连接到达。 |
| 配置服务 | 临时 HOME/USERPROFILE，CRUD/重新加载/默认配置、时间戳、数据隔离、权限、损坏/不可写配置、并发。 |
| GUI API | 配置错误、CRUD、设置、日志 ID/限额/增量/导出/并发、本地 SSH 连接、代理启动/重启/停止与最后使用时间。 |
| TUN | 内存设备/gVisor endpoint，CIDR/地址计算、真实包入站和出站、引用释放、空队列和关闭、netstack 前缀。 |
| React | 真实生成 Wails JS，仅替换 window.go/window.runtime 原生边界；配置CRUD/保存失败、状态轮询卸载/过渡、权限、主题/语言、导航、页面编辑/取消/日志筛选与导出。 |

本机 SSH fixture 只允许向 loopback 转发，`remote.test` 仅在 fixture 的 SSH 端解析。所有密钥和配置均为临时数据；测试不依赖外部 SSH 账号，不读取用户真实私钥，不修改系统代理、系统路由或管理员权限。

服务层少数测试通过短暂占用后释放端口选择可用地址，仍有其他进程抢占该端口的理论窗口。同包网络测试使用 `127.0.0.1:0` 与真实绑定地址。看到偶发 address-in-use 时应先核对并发进程，不应增加固定 sleep 或跳过用例。

## Fuzz 和重复运行

先 `go test` 确认种子通过，再按需要运行单个 fuzz target；长时间 fuzz 不放进每次默认测试。

```sh
go test ./internal/config -run '^$' -fuzz FuzzGetJumpHostInfo -fuzztime=10s
go test ./internal/router -run '^$' -fuzz FuzzDomainSuffixBoundary -fuzztime=10s
go test -race ./internal/proxy -run TestProxyCloseWhileConnectionsArrive -count=20
```

实际 fuzz 名称以对应测试文件或 `go test -list Fuzz ./internal/config ./internal/router ./internal/tun ./cmd/gotun/cli` 为准。

## 2026-10-19 GUI 交互补测

原生逐按钮记录、800×600/1024×700、公司连接证据及站点浏览器权限边界见 [GUI_INTERACTION_AUDIT.md](GUI_INTERACTION_AUDIT.md)。最终结果：

| 验证 | 结果 |
| --- | --- |
| 根模块 Go 1.25.5 race + coverage | 98 个顶层测试/fuzz 入口通过、3 个跳过；含子例 330 个通过事件，语句 52.8% |
| 独立 GUI Go 模块 race + coverage | 15 个顶层测试、含子例 32 个通过事件，语句 69.1% |
| 前端 Node 20 / 25 | Node 20 coverage、Node 25 普通全量各 125/125 通过、10 文件；Node 20 覆盖范围内行/语句 98.76%、分支 89.61%、函数 94.87% |
| 构建/静态检查 | tsc、Vite、macOS ARM64 Wails bundle、两模块 vet、diff --check 通过 |

前端现包含 Home 22、Profiles 26、Logs 25、Settings 15、providers 9、Layout 2、styles 4、hooks 22 项。CSS 实际进入测试环境，标签点击、Paper 背景、颜色对比断言能够抓住样式回归；jsdom 不测像素，原生窗口检查补足这部分。设置队列、保存失败回滚、迟到日志合并及清空确认均有回归。截图反馈后另补了操作图标垂直对齐、紧凑开关左边距与标签间距检查。Node 25 首次 coverage 有复杂表单用例触发默认 5 秒超时，Node 20 coverage 完整通过；没有增加超时或跳过断言来消除失败，详见交互报告的补测记录。

### 真实配置 HTTPS（显式启用）

预先在本机环境设置 `GOTUN_LIVE_CONFIG`（用户配置 JSON 路径）、`GOTUN_LIVE_PROFILE`（精确 profile ID）、`GOTUN_LIVE_URL`（已获授权的 HTTPS 地址），可选 `GOTUN_LIVE_EXPECTED_TITLE`（默认 IDE），然后运行：

```sh
go test -count=1 -timeout=120s -run '^TestLiveCompanyHTTPS$' -v ./internal/service
```

不要把密码、私钥、授权 URL 或真实配置复制进仓库。三个必需变量全空时明确跳过；部分变量缺失或网络/认证/响应校验失败时失败。测试不会修改配置、系统代理或路由；使用独立临时监听，保留真实 SSH/跳板，校验 CONNECT/TLS、200、HTML 和标题，拒绝跨 origin/明文重定向并移除 Referer。它不执行页面 JavaScript。

默认 suite 用本地受信/不受信 TLS 和 SSH fixture 覆盖上述判断。公司站点本轮浏览器重试仍被保存权限/受管策略阻止，没有通过 opt-in 测试绕过这次拒绝。

## 2026-10-18 初始全项目验证（历史）

此后 GUI 增量以以上 2026-10-19 结果为准；以下记录保留工具链与干净源码验证的历史证据：

| 验证 | 结果 |
| --- | --- |
| 根模块 Go 1.25.5，race + coverage | 92 个顶层测试/fuzz 入口通过，2 个跳过；包含子例共 306 个通过事件。 |
| GUI 独立 Go 模块，race | 15 个顶层测试通过，包含子例共 32 个通过事件。 |
| Go 1.26.0 | 根模块及 GUI 全量 race 通过；旧依赖的编译错误已通过移植 main 修复解决。 |
| React / Node 20.18.2 和 25.6.1 | 两版本各 54/54 测试通过，9 个测试文件；真实前端构建通过。 |
| 干净临时源码副本 | 不复制 node_modules/dist/Wails bindings，`make test-all` 完整执行通过。 |
| 关闭时连接并发到达 | race 模式重复 100 次通过；活动隧道/半关闭/fixture 清理另有重复回归。 |
| 静态检查 | 根模块和 GUI `go vet`、gofmt、`git diff --check` 通过。 |
| 构建/运行 | macOS ARM64 GUI 打包、CLI 帮助和版本、原生 GUI 保存配置/主页/日志检查通过；Go 1.26 下 Linux/Windows AMD64 CLI 交叉编译通过。 |

| 覆盖范围 | 语句覆盖率 |
| --- | --- |
| 根模块总计 | 52.7% |
| config | 98.4% |
| router | 98.3% |
| proxy（SSH/HTTP/SOCKS5） | 79.4% |
| service（含未启真实设备的 TUN 服务） | 69.1% |
| TUN 核心（内存设备路径） | 20.9% |
| sysproxy（只测地址归一化/未启用时关闭） | 8.7% |
| GUI App | 69.1% |
| React hooks/providers/pages/Layout | 行/语句 95.93%，分支 79.91%，函数 78.89% |

根模块总计包含没有单独测试入口的 `internal/testutil`，其代码实际由集成测试调用，但默认 Go 按包插桩没有将这些调用计入该包覆盖率。CLI 长时间主循环、系统命令、终端与特权路径也拉低总计。覆盖率是执行比例，不证明未运行的平台命令或特权行为正确。

Node 25 添加了原生 Storage 名称，而旧 Vitest 会优先保留 Node 的同名全局对象。测试环境包装仅在初始化/结束时移除并恢复这些描述符，实际仍使用 Vitest 的真实 jsdom Storage，没有用测试内存字典替换浏览器存储。

已知非失败警告：Vite 3 对 Material UI 7 的 `use client` 指令输出警告；JS chunk 约 522.63 KiB，现有 logo 约 5.7 MiB。部分 MUI 禁用按钮 Tooltip、失效配置选择测试会触发组件警告。没有通过静默 console 或忽略失败断言掩盖这些输出。

## 仍需人工或独立平台环境验证

- 真正的 macOS/Linux/Windows TUN 设备创建、提升权限及取消授权、默认网关保护、路由冲突/失败回滚、退出后路由恢复。
- 系统已有 HTTP/HTTPS/SOCKS/bypass 设置的启用与完整恢复，包括命令失败；这些目前仍有已知缺口，见项目分析。
- 原生文件/目录/保存对话框、窗口关闭/隐藏、托盘/自启动（后两者尚未实现）。
- 外部 SSH、多跳断线、长时间 keepalive、握手/目标超时、真实 DNS/NAT/跨网访问、TLS 流量和各种真实客户端。
- Windows/Linux 实机运行与 GUI 原生构建；本机交叉编译只验证可编译性。
- 交互终端密码读取的人工测试默认跳过；Linux 专属 fake-ip 命令测试在 macOS 跳过。

未推送工作分支，所以 CI 文件的改动还没有在 GitHub Actions 上执行。
