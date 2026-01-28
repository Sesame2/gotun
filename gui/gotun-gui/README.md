# GoTun GUI

GoTun GUI 是 [GoTun](https://github.com/Sesame2/gotun) 的图形用户界面客户端，基于 [Wails](https://wails.io) 框架构建，前端采用 React + TypeScript + Material UI

## 功能特性

- 🎨 **现代化界面**: 深色主题，简洁美观的 UI 设计
- 🔌 **SSH 配置管理**: 支持创建、编辑、删除 SSH 配置，支持密码和私钥认证
- 🚀 **一键连接**: 快速启动/停止代理服务
- 🔍 **连接测试**: 在启动前测试 SSH 连接是否正常
- 📊 **状态监控**: 实时显示代理状态、连接信息、运行时间
- ⚙️ **系统代理**: 自动设置/取消系统代理
- 🌐 **HTTP/SOCKS5**: 支持 HTTP 和 SOCKS5 代理协议

## 系统要求

### macOS
- macOS 10.15 (Catalina) 或更高版本
- 支持 Intel 和 Apple Silicon (ARM64) 架构

### Windows
- Windows 10 或更高版本
- 需要 WebView2 运行时（Windows 11 自带）

## 开发环境

### 前置要求

- Go 1.23 或更高版本
- Node.js 18+ 和 npm
- Wails CLI v2.10.1+

```bash
# 安装 Wails CLI
go install github.com/wailsapp/wails/v2/cmd/wails@latest

# 检查 Wails 环境
wails doctor
```

### 安装依赖

```bash
cd gui/gotun-gui

# 安装 Go 依赖
go mod tidy

# 安装前端依赖
cd frontend && npm install && cd ..
```

### 开发模式

```bash
cd gui/gotun-gui
wails dev
```

开发模式下：
- 自动热重载前端代码
- 自动重新编译 Go 代码
- 打开开发者工具调试

### 构建

```bash
cd gui/gotun-gui

# macOS
wails build

# Windows (需要在 Windows 环境或使用交叉编译)
wails build -platform windows/amd64

# 生成的应用位于 build/bin/ 目录
```

## 项目结构

```
gui/gotun-gui/
├── app.go              # Wails 应用主逻辑，Go 后端 API
├── main.go             # 应用入口
├── wails.json          # Wails 配置文件
├── go.mod              # Go 模块定义
├── build/              # 构建输出目录
│   └── bin/
│       └── GoTun.app/  # macOS 应用包
├── frontend/           # 前端代码
│   ├── src/
│   │   ├── App.tsx           # React 应用入口
│   │   ├── components/       # 通用组件
│   │   │   └── Layout/       # 布局组件（侧边栏导航）
│   │   ├── pages/            # 页面组件
│   │   │   ├── Home/         # 主页（控制面板）
│   │   │   ├── Profiles/     # 配置管理
│   │   │   ├── Logs/         # 日志查看
│   │   │   └── Settings/     # 设置
│   │   ├── hooks/            # React Hooks
│   │   │   ├── useConfig.ts       # 配置管理 Hook
│   │   │   └── useProxyStatus.ts  # 代理状态 Hook
│   │   ├── types/            # TypeScript 类型定义
│   │   └── styles/           # 全局样式
│   └── wailsjs/              # Wails 生成的 JS 绑定
│       └── go/
│           ├── main/App.js   # Go 方法的 JS 绑定
│           └── models.ts     # Go 类型的 TS 定义
└── internal/           # （在项目根目录）
    └── service/        # 服务层
        ├── proxy_service.go   # 代理服务
        └── config_service.go  # 配置服务
```

## API 说明

### Go 后端 API

GUI 应用通过 Wails 绑定调用以下 Go 方法：

| 方法 | 说明 |
|------|------|
| `StartProxy(profileId)` | 启动代理服务 |
| `StopProxy()` | 停止代理服务 |
| `RestartProxy()` | 重启代理服务 |
| `GetProxyStatus()` | 获取代理状态 |
| `TestConnection(profileId)` | 测试 SSH 连接 |
| `TestConnectionWithConfig(profile)` | 使用配置测试连接 |
| `GetProfiles()` | 获取所有配置 |
| `GetProfile(id)` | 获取单个配置 |
| `AddProfile(profile)` | 添加配置 |
| `UpdateProfile(profile)` | 更新配置 |
| `DeleteProfile(id)` | 删除配置 |
| `GetSettings()` | 获取应用设置 |
| `UpdateSettings(settings)` | 更新应用设置 |
| `GetDefaultProfile()` | 获取默认配置 |
| `OpenFileDialog(title, filters)` | 打开文件选择对话框 |
| `OpenDirectoryDialog(title)` | 打开目录选择对话框 |
| `GetVersion()` | 获取应用版本 |

### 数据结构

#### SSHProfile（SSH 配置）

```typescript
interface SSHProfile {
  id: string;           // 唯一标识
  name: string;         // 配置名称
  host: string;         // SSH 服务器地址
  port: string;         // SSH 端口
  user: string;         // 用户名
  password?: string;    // 密码（可选）
  keyFile?: string;     // 私钥文件路径（可选）
  jumpHosts?: string[]; // 跳板机列表（可选）
  httpAddr: string;     // HTTP 代理监听地址
  socksAddr?: string;   // SOCKS5 代理监听地址（可选）
  systemProxy: boolean; // 是否设置系统代理
  ruleFile?: string;    // 路由规则文件路径（可选）
  createdAt: Date;      // 创建时间
  updatedAt: Date;      // 更新时间
  lastUsedAt?: Date;    // 最后使用时间
}
```

#### ProxyStats（代理状态）

```typescript
interface ProxyStats {
  status: 'stopped' | 'starting' | 'running' | 'stopping' | 'error';
  httpAddr: string;       // HTTP 代理地址
  socksAddr?: string;     // SOCKS5 代理地址
  sshServer: string;      // SSH 服务器
  sshUser: string;        // SSH 用户
  jumpHosts?: string[];   // 跳板机列表
  systemProxy: boolean;   // 系统代理状态
  startTime?: Date;       // 启动时间
  uptime?: string;        // 运行时长
  errorMessage?: string;  // 错误信息
  totalRequests: number;  // 总请求数
}
```

## 配置文件

配置数据存储在：
- macOS/Linux: `~/.gotun/config.json`
- Windows: `%USERPROFILE%\.gotun\config.json`

## 使用说明

### 添加 SSH 配置

1. 打开应用后，点击左侧 "配置管理" 按钮
2. 点击 "添加配置" 按钮
3. 填写 SSH 服务器信息：
   - **配置名称**: 给配置起一个便于识别的名字
   - **SSH 主机**: 服务器地址或域名
   - **端口**: SSH 端口，默认 22
   - **用户名**: SSH 登录用户名
   - **密码/私钥**: 选择密码或私钥文件进行认证
   - **HTTP 代理地址**: 本地 HTTP 代理监听地址，如 `:8080`
   - **系统代理**: 是否自动设置系统代理
4. 点击 "保存"

### 启动代理

1. 在主页选择一个配置
2. 点击 "测试连接" 按钮验证 SSH 连接是否正常
3. 点击 "启动" 按钮开始代理
4. 状态卡片会显示当前代理状态和相关信息

### 设置系统代理

如果在配置中启用了 "自动设置系统代理"，启动代理后会自动配置系统的 HTTP 代理设置。

手动设置系统代理：
- **macOS**: 系统偏好设置 → 网络 → 高级 → 代理
- **Windows**: 设置 → 网络和 Internet → 代理

## 故障排除

### 连接测试失败

1. 检查 SSH 服务器地址和端口是否正确
2. 检查用户名和密码/私钥是否正确
3. 确保 SSH 服务器允许密码/密钥认证
4. 检查防火墙是否阻止了 SSH 连接

### 代理无法使用

1. 检查本地端口是否被占用
2. 确保没有其他程序占用相同的代理端口
3. 查看日志页面的错误信息

### macOS 安全警告

首次运行时可能提示 "无法验证开发者"：
1. 打开系统偏好设置 → 安全性与隐私
2. 点击 "仍要打开"

## 开发计划

- [ ] 支持多语言（英文）
- [ ] 支持浅色主题
- [ ] 添加系统托盘图标
- [ ] 支持开机自启动
- [ ] 支持流量统计
- [ ] 支持规则编辑器
- [ ] 支持跳板机配置
- [ ] 支持配置导入/导出

## 许可证

MIT License - 详见 [LICENSE](../../LICENSE) 文件

## 致谢

- [Wails](https://wails.io) - Go + Web 技术构建桌面应用
- [Clash Verge Rev](https://github.com/clash-verge-rev/clash-verge-rev) - UI 设计参考
- [Material UI](https://mui.com) - React UI 组件库
