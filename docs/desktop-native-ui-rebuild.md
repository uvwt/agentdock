# AgentDock Native Desktop UI 重构方案

> 状态：开发基线  
> 建立日期：2026-10-04  
> 基线分支：`origin/main@2de2f3d`  
> 开发分支：`refactor/desktop-native-ui-20261004`  
> 开发 worktree：`/Users/xx/Project/worktrees/agentdock-native-desktop-ui-20261004`

## 1. 背景

此前 PR #181（`feat/control-panel-ui-unification-20261002`）已经完成多轮 macOS / Windows 控制面板视觉统一探索，但最终验证出一个更根本的问题：

- 单纯调整 WPF / AppKit 的颜色、字号、卡片、padding 和页面结构，仍然很难得到目标中的现代原生桌面质感。
- Windows WPF 需要持续维护大量自定义样式，才能接近 Windows 11 / Fluent 的现代控件行为。
- macOS 继续完全依赖手工 AppKit 页面布局，也会让页面级 UI 维护成本偏高。
- 目标不是让两端像素级一致，而是让用户第一眼感受到“同一个 AgentDock”，同时第二眼仍然是各平台自然的原生应用。

因此本轮不继续在 PR #181 上补丁式迭代，而是从最新 `main` 重新建立现代原生桌面 UI。

PR #181 保持为设计探索和历史参考，不作为本轮代码基础，不整体 cherry-pick，也不要直接合并到本轮重构。

## 2. 参考产品与实际技术栈证据

本轮视觉参考重点是 CloudDrive2 的桌面端产品方法，不做一比一复刻。

2026-10-04 在真实设备上确认：

### macOS CloudDrive2

Mini 上的 `/Applications/CloudDrive.app` 主程序直接链接：

- `SwiftUI.framework`
- `AppKit.framework`
- `Combine.framework`

因此其主 UI 路线是 SwiftUI + AppKit 混合，而不是 Electron / Qt。

### Windows CloudDrive2

TianYi 上的 `C:\Program Files\CloudDrive\CloudDrive.exe`：

- ProductVersion：`1.0.18.1`
- .NET：`net10.0`
- Windows App SDK：`2.3.1`
- WinUI：`Microsoft.WindowsAppSDK.WinUI 2.3.0`

运行进程实际加载：

- `Microsoft.WinUI.dll`
- `Microsoft.UI.Xaml.dll`
- `Microsoft.UI.Xaml.Controls.dll`
- `Microsoft.WindowsAppRuntime.dll`

因此其 Windows 主 UI 路线明确是 WinUI 3 + Windows App SDK。

这些证据只用于确认“现代平台原生 UI”方向，不绑定 AgentDock 必须复制 CloudDrive2 的具体实现细节或版本。

## 3. 本轮目标

一句话目标：

> 以 CloudDrive2 一类现代原生桌面应用为视觉参考，Mac 与 Windows 保持统一的信息架构、视觉密度和组件语义，同时使用各平台原生设计语言与交互。

最终技术方向：

### macOS

- SwiftUI：页面、Sidebar、Settings、状态展示、列表、表单和主要页面组件。
- AppKit：窗口生命周期、菜单栏、系统权限、SMAppService、特殊窗口行为和其他底层系统集成。

### Windows

- WinUI 3：页面、NavigationView、Settings、状态展示、列表、表单和主要页面组件。
- .NET / Win32 / Windows App SDK：托盘、单实例、Runtime 管理、系统启动和其他平台能力。

共享的是产品规范和业务语义，不共享 UI 源码。

## 4. 不做什么

本轮明确避免以下方向：

- 不在 PR #181 上继续堆视觉补丁。
- 不把现有 WPF 页面逐步“伪装成 WinUI”作为最终方案。
- 不要求 Mac 和 Windows 像素级一致。
- 不自己重绘两端所有系统控件来强求完全一致。
- 不引入 Electron、Tauri、Avalonia、Qt 等跨平台 UI 框架。
- 不做 SaaS Dashboard 风格的 KPI 卡片墙。
- 不使用大面积 Hero、大字号标题、夸张渐变和重阴影。
- 不把每一条信息都包成独立卡片。
- 不一次性在 Settings 首页展开全部高级配置。
- 不为了迁 UI 重写已经稳定的 Runtime、配置、权限、更新等业务逻辑。

## 5. 跨平台统一原则

Mac / Windows 必须统一：

- 一级导航名称与顺序。
- 页面信息架构。
- 页面核心操作的位置和层级。
- Section 顺序。
- 状态表达语义。
- 品牌色和品牌图标。
- 视觉密度。
- 主要 spacing 体系。
- 设置项分组方式。
- 空状态、错误状态、运行状态的产品语义。
- 中英文文案语义。

允许平台原生差异：

- Title bar 和窗口按钮。
- SF Symbols / Segoe Fluent Icons 的具体 glyph。
- 字体实际渲染。
- Toggle、Context Menu、Scrollbar 等系统控件外观。
- Mica / macOS 系统材质。
- Hover / Pressed 动画细节。
- 平台特有窗口生命周期行为。

原则：

> 布局和产品语义必须统一，原生控件不强行画成同一种外观。

## 6. 整体 App Shell

一级导航固定为：

1. 主页
2. 连接
3. 能力
4. 活动
5. 设置

整体结构：

```text
┌───────────────────────────────────────────────────────┐
│ AgentDock                                             │
├──────────────┬────────────────────────────────────────┤
│              │                                        │
│  主页         │  页面标题                              │
│  连接         │  页面说明                              │
│  能力         │                                        │
│  活动         │  页面主体                              │
│              │                                        │
│  设置         │                                        │
│              │                                        │
└──────────────┴────────────────────────────────────────┘
```

设计约束：

- Sidebar 紧凑，不做大号导航按钮。
- 页面标题克制，不做 24–30px 的网页 Hero。
- 主内容优先使用 Grouped List / Section / Divider 形成层级。
- 阴影默认不用。
- 圆角主要服务分组 surface，不作为装饰到处出现。
- 品牌蓝只用于 Primary Action、Toggle On、选中态、链接和状态强调。

## 7. 设计 Token 基线

以下值作为第一轮实现基线，允许真机截图后小范围校准，但不允许双端各自随意漂移：

| Token | 基线 |
|---|---:|
| 默认窗口 | 1060 × 720（逻辑尺寸：macOS point / WinUI DIP） |
| 最小窗口 | 900 × 620（逻辑尺寸） |
| 一级 Sidebar | 168 |
| 页面水平 Padding | 28 |
| 页面顶部 Padding | 24 |
| 页面标题 | 20 / Semibold |
| 页面说明 | 12–13 |
| Section 间距 | 20–24 |
| Section 圆角 | 10 |
| 普通设置行 | 42–46 |
| 设置行水平 Padding | 12–14 |
| 标准按钮高度 | 30–32 |
| 小按钮高度 | 26–28 |
| Icon | 15–17 |
| 状态 Badge | 18–20 |

优先 spacing：

- 4
- 8
- 12
- 16
- 24
- 32

避免页面自己发明零散间距。

## 8. 核心组件语义

两端分别实现自己的原生组件，但语义保持一致。

需要建立的最小组件集合：

- `AppShell`
- `SidebarItem`
- `PageHeader`
- `SettingsSection`
- `SettingsRow`
- `StatusBadge`
- `InlineAction`
- `EmptyState`

不要提前建立大而全的 Design System。

只有真实重复出现并能降低理解成本的 UI 模式才提取组件。

## 9. Grouped Section

Grouped Section 是本轮最重要的视觉基础。

目标形态：

```text
Runtime

┌────────────────────────────────────────────┐
│ AgentDock Runtime               正在运行 ● │
├────────────────────────────────────────────┤
│ 监听地址                    127.0.0.1:8765 │
├────────────────────────────────────────────┤
│ 自动启动                              [●]  │
└────────────────────────────────────────────┘
```

要求：

- 一个 Section 对应一个稳定业务概念。
- Section 内使用行和 Divider 组织信息。
- 不把每个 Row 再包独立卡片。
- Row 中优先左侧标题 / 说明，右侧状态 / 控件 / 操作。
- 同一 Section 内不堆叠不相关能力。

## 10. 页面方案

### 10.1 主页

主页只回答两个问题：

1. AgentDock 当前是否正常。
2. 用户现在最需要做什么。

建议结构：

```text
主页

AgentDock Runtime

● 正在运行
本地服务已准备好，AI 客户端可以连接。

[停止]

运行状态

┌────────────────────────────────────────────┐
│ Runtime                         正在运行 ● │
├────────────────────────────────────────────┤
│ MCP                            已准备就绪 ● │
├────────────────────────────────────────────┤
│ NexusDock                       已连接 ●   │
└────────────────────────────────────────────┘

快速入口

连接 AI 客户端                          >
管理能力                              >
查看活动                              >
```

约束：

- 不做 KPI 卡片。
- 主操作动态切换：运行时“停止”，停止时“启动”。
- 状态信息保持一屏可读。

### 10.2 连接

建议结构：

```text
连接

让 AI 客户端连接这台设备。

本机连接

┌────────────────────────────────────────────┐
│ Local MCP                                  │
│ http://127.0.0.1:xxxx              [复制] │
├────────────────────────────────────────────┤
│ Public MCP                                 │
│ 当前未启用                           设置 > │
└────────────────────────────────────────────┘

远程连接

┌────────────────────────────────────────────┐
│ NexusDock                           已连接 ●│
│ 从 ChatGPT、Claude 等远程访问这台设备    > │
└────────────────────────────────────────────┘
```

公网检测属于 Public MCP 的状态，不单独做大卡片。

### 10.3 能力

建议结构：

```text
能力

配置 AgentDock 可以提供给 AI 的能力。

┌────────────────────────────────────────────┐
│ Browser                          已启用 ●  │
│ 浏览器自动化与网页操作                    │
│                                      >     │
├────────────────────────────────────────────┤
│ Coding Agent                     已启用 ● │
│ Codex、Claude Code 等编码代理              │
│                                      >     │
├────────────────────────────────────────────┤
│ MCP                              3 个服务 │
│ 外部 MCP Server 与 Plugin                 │
│                                      >     │
└────────────────────────────────────────────┘
```

详细配置进入对应能力的详情，不在首页展开成配置墙。

### 10.4 活动

Activity 是本机运行分析的唯一 UI，不再由 Core 额外维护浏览器页面。页面直接消费 `/internal/runtime/analytics`，保持 Runtime 数据模型为唯一事实来源。

建议结构：

```text
活动

调用概览
总调用 / 失败调用 / 进行中调用 / 统计窗口

调用统计
工具名 · 调用数 · 错误率 · P50 / P95 / P99

最近调用
工具名 · 来源 · 时间 · 耗时 · 状态
  └─ 展开：阶段耗时 / 错误码

运行资源
Go Heap / Heap In Use / Goroutine / GC / Uptime

Runtime
状态 / 版本 / 最后刷新

诊断
日志目录                              打开 >
配置目录                              打开 >
```

约束：

- 页面可见时约每 2 秒刷新，但不能因为刷新重建全部展开行并打断用户查看。
- 只展示 Runtime 已保存的安全元数据；不得展示参数、结果、命令、文件内容或错误正文。
- Home 只保留最近 3 条摘要，完整历史与性能统计统一进入 Activity。
- Core 保留 `/internal/runtime/analytics` 数据接口，但不再提供 `/analytics` 与 `/analytics/data` HTML/JSON 浏览器表面。

### 10.5 设置

设置页采用二级导航。

固定顺序：

1. Runtime
2. Permissions
3. Startup
4. Access Credentials

宽窗口建议结构：

```text
┌───────────┬──────────────────┬──────────────────────────┐
│ 一级导航   │ 设置导航          │ 内容                     │
│           │                  │                           │
│ 主页       │ Runtime          │ Runtime                   │
│ 连接       │ Permissions      │                           │
│ 能力       │ Startup          │ ┌───────────────────────┐ │
│ 活动       │ Access           │ │ ...                   │ │
│           │                  │ └───────────────────────┘ │
│ 设置       │                  │                           │
└───────────┴──────────────────┴───────────────────────────┘
```

要求：

- 不在 Settings 首页同时铺四张大卡。
- 当前设置类别右侧只展示当前内容。
- 窗口变窄时可把二级导航收敛成平台自然的 selector / compact navigation。
- 不为了保持三栏强行造成横向拥挤。

## 11. macOS 实现边界

目标目录形态可以逐步向以下结构靠拢，但不要为了目录整齐一次性机械搬文件：

```text
AgentDockApp
├── AppKit Host
│   ├── App lifecycle
│   ├── Menu bar
│   ├── NSWindow
│   ├── Permissions
│   └── SMAppService
│
└── SwiftUI Control Panel
    ├── AppShell
    ├── HomeView
    ├── ConnectionsView
    ├── CapabilitiesView
    ├── ActivityView
    ├── SettingsView
    └── Components
```

迁移原则：

- 现有 AppKit 的稳定系统能力优先保留。
- 新页面优先 SwiftUI。
- 不为了测试或视觉统一重写权限、服务控制、更新等稳定逻辑。
- SwiftUI 只负责 UI 状态和交互编排，业务能力通过明确边界调用现有服务。
- 需要 AppKit 才可靠的能力继续使用 AppKit，不为“纯 SwiftUI”目标牺牲工程质量。

## 12. Windows 实现边界

目标方向：

```text
AgentDock.ControlPanel
├── Platform
│   ├── Tray
│   ├── SingleInstance
│   ├── Startup
│   └── RuntimeControl
│
└── WinUI
    ├── AppShell
    ├── HomePage
    ├── ConnectionsPage
    ├── CapabilitiesPage
    ├── ActivityPage
    ├── SettingsPage
    └── Controls
```

迁移原则：

- 新 WinUI 3 Shell 与旧 WPF 控制面板允许阶段性并存。
- 先复用现有业务服务，不先重写 RuntimeService 等稳定逻辑。
- 完成功能对齐和安装链路验证之前，不删除旧 WPF。
- 不为了 WinUI 迁移改变 Runtime 协议和业务数据模型。
- WinUI 页面只负责呈现和用户交互，系统能力继续保持清晰的服务边界。
- 如果旧 WPF code-behind 混有业务逻辑，迁移时只抽取真正属于业务 / 平台服务的部分，不机械制造大量 interface。
- 原生 UI 重构阶段曾以 `Microsoft.WindowsAppSDK 2.3.1` 完成 TianYi 真机视觉与稳定性验证；随后 Windows 发行架构收口时，为匹配 Microsoft 公开稳定 Runtime installer，发行基线调整为 Windows App SDK `2.1.3` 对应的精确组件组合。此前试用的 `1.6.250602001` 在 TianYi 上即使最小窗口也会触发 native heap corruption，因此仍不采用 1.6。
- 新 WinUI 壳必须声明 `PerMonitorV2` DPI awareness。窗口设计尺寸按 DIP 处理，再根据 `GetDpiForWindow` 转换为物理像素；在工作区不足时允许 Windows 按系统规则压缩实际物理高度。
- Settings 不在 XAML 中直接设置 `SelectedIndex` 触发初始化期 `SelectionChanged`。默认选中项在页面导航完成后设置，并对未完成绑定的内容区做初始化保护。

## 13. 从 PR #181 搬什么，不搬什么

### 可以人工参考 / 搬运

- 五个一级导航。
- 页面内容顺序。
- 中英文文案语义。
- AgentDock 品牌资源。
- Runtime / Permissions / Startup / Access Credentials 顺序。
- Home 动态主操作。
- 已经验证合理的业务测试意图。
- 页面信息架构和已确认的产品决策。

### 不直接搬运

- #181 的 WPF 视觉 Style / ControlTemplate。
- #181 的 AppKit 页面布局代码。
- 为旧 UI 结构建立的大量卡片和 padding token。
- 为匹配旧截图加入的页面级 patch。
- 13 个提交整体 cherry-pick。

原则：

> 搬产品决策，不搬旧 UI 实现。

## 14. 开发阶段

### Phase 0：基线调查

先读取真实 main 代码，明确：

- Mac 窗口创建、菜单栏和 SetupWindow 的现状。
- Windows App.xaml / MainWindow.xaml / RuntimeService / 单实例现状。
- 安装器、签名、更新、托盘依赖。
- 现有 UI tests 哪些绑定旧实现，哪些表达真实业务契约。

不要先写 UI 再反推业务边界。

### Phase 1：双端空 Shell

只建立：

- 原生窗口。
- AgentDock 品牌。
- 一级 Sidebar。
- 五个导航入口。
- 空页面占位。
- 双端统一尺寸与 spacing 基线。

此阶段就要截图对比。

### Phase 2：Home + Settings

优先完成 Home 和 Settings。

原因：

- Home 可以验证主 Shell、状态层级和主操作。
- Settings 可以验证 Grouped Section、二级导航、信息密度和原生控件。

完成后必须在 Mac / TianYi 上真机截图并排审查。

如果这两个页面仍然没有达到目标，不继续后三页，先修设计基线。

### Phase 3：Connections + Capabilities + Activity

在 Phase 2 视觉方向确认后，再继续剩余页面。

2026-10-04：Home / Settings 视觉方向已由用户确认，Phase 3 已按同一视觉基线实现。

- Connections：双端统一为本机连接（Local / Public MCP）与远程连接（NexusDock），不扩展为卡片墙。
- Capabilities：双端统一展示 Browser、Coding Agent、MCP Apps 的真实配置状态。
- Activity：双端统一展示 Runtime 状态、版本、最近状态刷新时间，以及日志 / 配置目录入口。
- 页面继续读取现有 ServiceController / RuntimeService 与配置模型，没有建立第二套 Runtime 状态。

### Phase 4：功能完整性对齐

核对旧控制面板已有行为：

- Runtime start / stop。
- 地址与状态刷新。
- 配置保存。
- 权限入口。
- 登录启动。
- Access Credentials。
- NexusDock / Public MCP。
- 日志与配置目录。
- 更新链路。
- 托盘 / 单实例 / 激活行为。

### Phase 5：替换旧 UI

只有在：

- 双端功能对齐。
- 真机构建通过。
- 安装包链路通过。
- 截图确认。
- 自动化测试通过。
- 旧 UI 没有独有功能。

之后才删除旧 WPF / 旧页面实现。

## 15. 验证要求

每个阶段都必须真实验证，不接受只看编译。

通用：

- `git diff --check`
- `go test ./... -count=1 -timeout=3m`
- `go test ./scripts/test -count=1`

macOS：

- Swift / App 真实构建。
- 需要时跑 ZIP / DMG。
- 真机启动。
- 五页 UI 截图。
- 关键系统能力仍可用。

Windows：

- TianYi 使用 Visual Studio Build Tools 2022 的 MSBuild 做 WinUI 3 `Release x64` 真机构建。不要把裸 `dotnet build` 作为 WinUI 验证入口；TianYi 的 .NET SDK 路径缺少 `Microsoft.Build.Packaging.Pri.Tasks.dll`，会产生 `MSB4062`，而 VS MSBuild 可正常完成 PRI / XAML 构建。
- 真机启动。
- 五页 UI 截图。
- Runtime start / stop。
- 单实例 / 托盘 / 启动行为。
- 安装器兼容性。

### 15.1 2026-10-04 Phase 1/2 真机验证记录

当前 Phase 0～2 已完成实现和第一轮真机验证，Home / Settings 视觉方向已经用户确认并作为 Phase 3 基线。

已确认：

- macOS 新 SwiftUI Shell 可编译，Home / Settings backing-store 快照均已生成，最终窗口外框为 `1060 × 720`。
- Windows 新 WinUI 3 Shell 在原生 UI 重构阶段使用 `Microsoft.WindowsAppSDK 2.3.1` 完成 TianYi `Release x64` 构建和 Home / Settings 启动验证；后续发行基线改为公开稳定的 Windows App SDK `2.1.3` 组件组合，并重新执行双架构构建与安装链验证。
- TianYi 上 AgentDock WinUI 已从 DPI-unaware 的 `96 DPI` 修正为 `PerMonitorV2 / 192 DPI`。
- 同一窗口在 DPI-unaware 外部进程中看到的虚拟化矩形约为 `1060 × 667`，切换测量线程到 PerMonitorV2 后真实物理矩形为 `2120 × 1334`；宽度正确对应 1060 DIP，实际高度受当前 Windows 工作区限制而被系统压缩。
- Windows `SettingsPage` 初始化期崩溃已定位为 XAML `SelectedIndex` 过早触发 `SelectionChanged`，修复后 Home / Settings 均可稳定启动。
- Windows App SDK 1.6 在 TianYi 上最小 WinUI 窗口仍会 native crash，因此不再作为本轮基线。
- 旧 WPF 控制面板仍保留，新 WinUI 仅作为迁移中的新前端，未删除旧系统能力。

### 15.2 2026-10-04 Phase 3 实现与真机验证记录

Phase 3 的 Connections / Capabilities / Activity 已完成双端实现，信息架构保持一致。

macOS：

- SwiftUI 三页直接读取现有 ServiceStatus / ServiceConfiguration，并通过 ServiceController 复用日志与配置目录能力。
- i18n 检查、Swift 编译、App 构建、ZIP、DMG、ad-hoc signing、ZIP 解包校验与 DMG mount 校验均通过。
- 独立预览 App 已通过 LaunchServices 真机启动，并由 CGWindowList 确认存在真实 Layer 0 窗口。
- 当前 AgentDock 进程没有 Screen Recording 与 Accessibility 权限，因此本轮无法对 Phase 3 三页完成新的 Mac 屏幕截图 / AX 自动切页；在权限恢复前不得把 Mac Phase 3 视觉截图审查标记为完成。

Windows TianYi：

- 使用 Visual Studio Build Tools 2022 的 MSBuild 对 WinUI 3 Release x64 完成真实 Rebuild，PRI / XAML / C# 均实际参与构建。
- 通过当前 Administrator console session 1 的 InteractiveToken 启动 WinUI，跨命令确认窗口持续存活并取得真实 MainWindowHandle。
- 通过 UI Automation 按 NavigationView AutomationId 真实切换 Connections / Capabilities / Activity，三页均读取到 TianYi 当前 Runtime 配置与状态。
- Connections 实测读取 Local MCP、Public MCP、NexusDock endpoint 与连接状态；Capabilities 实测读取 Browser、Coding Agent 与 MCP Apps 配置；Activity 实测读取 Runtime、版本与诊断入口。
- 三个 Phase 3 页面均完成 TianYi 真机截图。
- 旧 WPF Release Rebuild 同机通过，迁移尚未破坏旧控制面板构建。

Phase 4 parity 初查：

- 新原生页面已经覆盖 Runtime start / stop、基础地址与状态、权限入口、Startup 状态、凭据保护状态、Nexus / Public MCP 状态、日志与配置目录入口。
- 旧 WPF 仍独有 Public MCP 模式与 Named Tunnel 编辑、Nexus 配对、Browser / CDP 编辑、Coding Agent Profile 编辑、MCP Apps 模式修改、端口 / 日志级别 / 语言保存与重启、凭据明文查看、更新等操作。
- 因此当前明确未达到删除旧 WPF 的条件；Phase 4 必须先完成这些可编辑能力和托盘 / 单实例 / 更新链路等行为对齐。

### 15.3 2026-10-04 Phase 4/5 parity 与替换完成

在 Phase 3 之后继续完成了原生控制面板的功能迁移，不再把旧 WPF / AppKit 高级设置作为功能兜底。

macOS SwiftUI：

- Connections 已覆盖 Local / Quick / Named Public MCP 模式、Named Tunnel 地址与 Token、NexusDock 配对。
- Capabilities 已覆盖 Browser 开关与连接模式、CDP 地址、Coding Agent 全局开关、内置 Profile、自定义 Profile、默认 Profile、MCP Apps 模式。
- Settings 已覆盖 Runtime 端口、日志级别、界面语言、更新入口、Core / 菜单栏 Startup、凭据显示。
- 继续复用 InstallerRunner、ServiceConfigurationController、ServiceController、SMAppService 与既有更新链路。
- 旧 AdvancedSettingsWindowController 已删除；SetupWindowController 只保留首次安装 / 修复所需流程，不再承载安装后的高级设置页。

Windows WinUI 3：

- Connections / Capabilities / Settings 已覆盖旧 WPF 的 Public MCP、Nexus 配对、Browser / CDP、Coding Agent Profile、MCP Apps、端口、日志、语言、Startup、管理员模式、凭据和更新能力。
- WinUI App 已接管 tray、single-instance、background 启动、Core / Tunnel startup helper、管理员命令 host、更新 handoff 恢复。
- 关闭主窗口时只隐藏窗口并保留 tray 进程；第二实例通过命名事件唤起已有窗口。
- 发布工作流和 Windows Installer 工作流已切换为发布 WinUI AgentDock.WinUI.csproj，输出继续保持 agentdock-tray.exe 契约。
- 旧 WPF UI 项目已删除；稳定的 Runtime / Models / Resources / Diagnostics 已移动到 desktop/windows/shared，由 WinUI 直接复用。

TianYi parity 验证：

- 最终 WinUI x64 Release 使用 Visual Studio Build Tools 2022 MSBuild 完整 Rebuild 通过；ARM64 使用 win-arm64 RID 的交叉 Rebuild 也通过。
- 最终验证快照 SHA-256：1231c12c9bb5e027bb04c589957551f3c17a83c54425e6ce78c83db209e24123。
- Session 1 真机启动得到 agentdock-tray.exe 原生窗口；关闭窗口后进程仍存在，再次启动第二实例后进程数保持 1，已有窗口重新显示。
- UI Automation 已重新切换 Connections / Capabilities / Activity / Settings，并读取到 TianYi 当前真实 Runtime / Nexus / Browser / ACP 配置。

到此，旧 WPF 已不再承担产品 UI，Windows 原生控制面板正式以 WinUI 3 为发布入口。

### 15.4 2026-10-04 最终验收修复

针对最终验收暴露出的本地化和交互问题继续收口：

- macOS / Windows Settings 二级导航不再硬编码英文；中文模式下分别展示运行时、权限、启动 / 开机启动、访问凭据。
- macOS 界面语言改为 Picker 选择后立即写入语言偏好并触发 SwiftUI 视图刷新，不再依赖 Runtime 保存或 App 重启。
- Windows 界面语言改为进程内切换资源文化并重建当前 WinUI 主窗口，绕开 single-instance 对“启动第二实例再退出旧实例”方案的竞态。
- Windows 原生页面剩余主要用户可见文案补齐 UiText 资源，确保语言切换后不仅一级导航变化，Settings / Home / Connections / Capabilities / Activity 也同步切换。
- Windows Home 三个“快速入口”从纯展示 Grid 改为真实 Button，并通过主 NavigationView 路由到 Connections / Capabilities / Activity，保证页面内容与左侧选中项同步。
- macOS SwiftUI 的 Browser、Coding Agent、Profile、Core Startup、Menu bar Startup 等布尔 Toggle 显式使用 switch 样式。

验收证据：

- Windows 最终源码快照 SHA-256：ad95eff497d118bf382df3431ff6b152f0c422fdb4160a694e65279323622abf。
- TianYi x64 Release 使用指定 VS Build Tools MSBuild 完整 Rebuild 通过；win-arm64 交叉 Rebuild 也通过。
- TianYi UI Automation 实测语言从中文切到 English 后，一级导航与 Settings 二级导航立即变为英文；再切回简体中文后立即恢复为“主页 / 连接 / 能力 / 活动 / 设置”和“运行时 / 权限 / 开机启动 / 访问凭据”，偏好最终保留为 zh-CN。
- TianYi UI Automation 实际触发三个快速入口后，ConnectionsNavigationItem / CapabilitiesNavigationItem / ActivityNavigationItem 均成为选中状态。
- macOS 完整 App / ZIP / DMG / 签名 / 挂载测试通过；最终 Preview App 已真实启动并出现 Layer 0 原生窗口。由于 Mini 当前仍未授予 Screen Recording / Accessibility，无法对新版 Mac 页面做截图或 AX 点击，因此不伪造视觉交互结论。

### 15.5 2026-10-04 NexusDock 优先连接体验收敛

本轮从 `f5cfad0` 新开独立 follow-up worktree，不继续污染已完成验收的原生 UI 分支：

- worktree：`/Users/xx/Project/worktrees/agentdock-native-connection-ux-20261004`
- branch：`refactor/desktop-native-connection-ux-20261004`
- baseline：`f5cfad0 fix(desktop): 收口原生面板验收交互`

产品信息架构按“普通用户优先 NexusDock，技术连接能力降级到高级设置”收敛：

- Home 不再分别展示 Runtime / MCP。底层继续保留进程运行与健康检查两个信号，但概览只汇总为一个 AgentDock 状态，并单独保留 NexusDock 连接状态。
- AgentDock 已启动但健康检查失败时，Home 显示“运行异常 / Needs attention”，启动/停止按钮仍按真实进程状态判断，避免误显示“启动”。
- Connections 默认只突出 NexusDock 推荐连接路径。
- Local MCP 地址、Authentication Token、OAuth Password 全部迁入“高级连接设置”。
- Public MCP 的 Local / Quick / Named 模式选择从产品 UI 移除；底层枚举继续兼容已有配置。
- 高级公网直连只向用户呈现 Cloudflare Tunnel 临时公网地址与固定域名两种操作；点击操作时直接写入 quick / named 底层模式。
- Settings 二级导航不再暴露 Access Credentials，也不再展示 Local MCP 地址；后续扩展为 Runtime / Permissions / Startup / Appearance / About，其中 Appearance 负责主题与语言，About 负责版本、更新与项目资源。

验证：

- `python3 scripts/test/check-macos-i18n.py`、Windows UiText 资源覆盖检查、`go test ./scripts/test -count=1` 均通过。
- `go test ./... -count=1 -timeout=3m` 全仓通过。
- macOS `scripts/test/test-macos-app.sh` 完整通过 App / ZIP / DMG / 签名 / 挂载验证。
- Windows 验证源码快照 SHA-256：`7fe2c360704f4013319868d9c97c7772e4d0c15c483fad9bd98b3ed89fb4289c`；TianYi x64 Release Rebuild 0 warning / 0 error，win-arm64 交叉 Rebuild 退出码 0。
- TianYi UI Automation 实测 Home 只存在 AgentDock / NexusDock，不再出现 Runtime / MCP；当前真实状态显示 AgentDock 正在运行、NexusDock 已连接。
- TianYi UI Automation 实测 Connections 的高级连接设置默认 `Collapsed`；展开后才出现 Local MCP、访问凭据、Cloudflare Tunnel、固定域名；`仅本地` / `Public MCP` 不再出现。
- TianYi UI Automation 实测 Settings 只剩运行时 / 权限 / 开机启动，`CredentialsNavigationItem` 不存在。

### 15.6 2026-10-04 Settings 外观与关于

在 NexusDock 优先连接体验稳定后，进一步按桌面应用常规信息架构整理 Settings：

- Settings 二级导航调整为 Runtime / Permissions / Startup / Appearance / About。
- Interface language 从 Runtime 迁移到 Appearance。
- Appearance 提供 Follow system / Light / Dark 三档主题；macOS 使用 NSAppearance 即时应用并通过 UserDefaults 持久化，Windows 使用 WinUI ElementTheme 即时应用并通过 LocalAppData 持久化。
- Windows 切换语言后重建主窗口时保留 Settings → Appearance 当前页，不再跳回 Runtime。
- Check for updates 从 Runtime 迁移到 About，避免运行时配置和应用级更新混在一起。
- About 保留当前版本、文档、GitHub，并新增 Check for updates。

验证：

- macOS i18n 374 个引用均覆盖，Windows UiText 142 个引用均覆盖，`go test ./scripts/test -count=1` 通过。
- macOS `scripts/test/test-macos-app.sh` 完整通过 App / ZIP / DMG / 签名 / 挂载验证。
- Windows 最终验证快照 SHA-256：`f2f20ab5b42fdaaead9b0113552de362ddf5164cc8641ba4af905793772b4393`；TianYi x64 Release Rebuild 与 win-arm64 交叉 Rebuild 均成功。
- TianYi UI Automation 实测 Runtime 页面不再出现界面语言和检查更新；Appearance 页面同时出现主题和界面语言；About 页面存在版本和“检查更新…”按钮。
- TianYi UI Automation 实测主题依次切换到 Dark / Light 后偏好分别持久化为 `dark` / `light`，恢复 Follow system 后 `ui-theme` 偏好文件被移除。
- TianYi UI Automation 实测中文切到 English 后仍停留在 Appearance，再切回简体中文后仍停留在外观；最终语言偏好恢复为 `zh-CN`，主题恢复 Follow system。

### 15.7 2026-10-04 远程连接语义与高级设置分层

在 15.5 的基础上继续减少普通用户需要理解的产品概念：

- 用户可见主概念从 NexusDock 收敛为“远程连接”；主页状态项同步改为“远程连接”，不再要求用户先理解 NexusDock 品牌名称。
- 连接页负责“连接到哪里”和“完成配对”：默认使用官方服务 `https://mcp.nexusdock.co`，界面显示“官方服务 · nexusdock.co”；点击“更改”后才出现官方服务 / 自托管服务选择，自托管时显示服务地址输入框。
- 官方服务下补充 NexusDock 设备页入口：未配对时提示用户前往 `https://mcp.nexusdock.co/workspace/devices` 获取配对码；已配对时改为“管理已连接设备”。自托管服务不显示该入口，避免把官方 SaaS 流程混入自托管配置。
- 连接页不再承载 Local MCP、访问凭据或 Cloudflare Tunnel 配置。“高级连接设置”改为单一跳转入口，直接进入“设置 → 高级连接”。
- 设置二级导航调整为“运行时 / 权限 / 开机启动 / 高级连接”。高级连接统一承载 Local MCP、Authentication Token、OAuth Password、Cloudflare 临时公网地址与固定域名。
- 底层仍复用现有 Nexus pairing 与 tunnel `none / quick / named` 配置，不为了 UI 命名变化改写稳定运行协议。
- Windows 最终验证源码快照 SHA-256：`cfcf558cc707cccbfd812ddb7211129426b1a04ce835672ea51206a8e8ddf917`；TianYi 使用指定 VS 2022 Build Tools MSBuild 完成 x64 与 win-arm64 Release Rebuild。
- TianYi UI Automation 实测：连接页默认显示“官方服务 · nexusdock.co”；点击“更改”后可切到自托管并出现服务地址，输入测试地址后摘要即时更新；恢复官方服务后未执行配对，不修改现有设备身份。点击“高级连接设置”后，一级导航切到“设置”、二级导航选中“高级连接”，并真实出现 Local MCP、本地地址、认证令牌、OAuth 密码、公网访问、Cloudflare Tunnel 和固定域名。
- macOS 完整 App / ZIP / DMG / 签名 / 解包 / 挂载验证通过；最终源码的 i18n 检查为 370 个引用 key、每个 locale 453 个条目。

### 15.8 2026-10-04 Runtime 页面职责收敛

首页已经负责 AgentDock 的运行健康与启动 / 停止入口，因此 Settings → Runtime 不再重复展示状态和服务控制：

- Runtime 页面只保留服务端口、日志级别和“应用更改”。
- “保存并重启 Runtime”改为“应用更改”，并明确提示“更改运行参数后将自动重启 AgentDock”。
- 底层行为没有改变：保存端口 / 日志级别后仍会执行 Runtime restart，使新参数立即生效。
- Windows 删除 Runtime 页面专用的 Start / Stop 事件处理；首页仍保留 AgentDock 启停入口。
- macOS / Windows 都继续把应用级“检查更新”留在 About，把主题和语言留在 Appearance。

验证：

- macOS i18n、Windows UiText 资源覆盖和 `go test ./scripts/test -count=1` 通过。
- macOS `scripts/test/test-macos-app.sh` 完整通过 App / ZIP / DMG / 签名 / 解包 / 挂载验证。
- Windows 最终验证快照 SHA-256：`d606e5ba87d6308327d18e45a12a8da455121a03b4ad82ffc6d024363df72a7a`；TianYi x64 Release 与 win-arm64 交叉 Rebuild 均成功。
- TianYi UI Automation 实测 Runtime 页面不存在“状态 / 服务 / 启动 / 停止”，存在“端口 / 日志级别 / 配置 / 应用更改”，并显示“更改运行参数后将自动重启 AgentDock。”。

### 15.9 2026-10-04 Home 信息层级重构

Home 从“状态卡 + 快捷入口卡”的简单设置页形态，收敛为真正的设备控制中心：

- 顶部只表达 AgentDock 整体运行状态和启动 / 停止动作，不再重复 Runtime / MCP 实现层概念。
- 远程连接独立成一行，显示真实连接状态和当前远程服务；官方服务显示“官方服务 · nexusdock.co”，自托管显示实际 Endpoint。
- 能力区直接读取真实配置，不复制第二套状态：
  - Browser 使用 `browser_enabled`。
  - Coding Agent 同时要求 ACP 总开关开启、存在至少一个启用 Profile，并且默认 Profile 指向启用项；配置不完整时显示“需要处理”。
  - MCP Apps 根据 `full / compact / off` 映射为启用 / 关闭；未知模式显示“需要处理”。
- 能力摘要按真实状态自适应：全部启用显示“全部可用”，部分启用显示“x / 3 可用”，全部关闭显示“尚未启用”，存在异常显示“x 项需要处理”。
- Home 底部只保留轻量的连接 / 能力 / 活动导航，不再使用三行 Quick access 卡片。
- 当前 Runtime 没有可供 Home 安全消费的结构化“最近 AI 活动”接口，Activity 页面本身也仍是运行诊断，因此本轮不伪造最近活动。等后续有正式 Activity 数据源再接入 Home。

验证：

- macOS i18n 为 385 个引用 key、每个 locale 478 个条目；`go test ./scripts/test -count=1` 与 `go test ./... -count=1 -timeout=3m` 均通过。
- macOS `scripts/test/test-macos-app.sh` 完整通过 App / ZIP / DMG / 签名 / 解包 / 挂载验证。
- Windows 最终验证快照 SHA-256：`b685d12e3cc33d14ce2008e8f1fedc5516f7c565b2c9bf76f2fc5961a3af7c70`；TianYi x64 Release 与 win-arm64 交叉 Rebuild 均成功。
- TianYi UI Automation 实测当前真实状态：AgentDock 正在运行、远程连接已连接到“官方服务 · nexusdock.co”、Browser / Coding Agent / MCP Apps 均为启用，摘要显示“全部可用”。
- TianYi UI Automation 实测 Home 不再出现“运行时 / MCP / 快速入口”，底部保留“连接 / 能力 / 活动”轻量入口。

### 15.10 2026-10-04 能力页扩展概要与产品边界

长期职责按“AgentDock 是 Runtime，NexusDock 是多设备控制面”继续收敛，不新增一级“扩展”导航，也不在 SwiftUI / WinUI 复制 NexusDock 已有的 Skill / Plugin / MCP 完整管理中心：

- “能力”页拆成“内置能力”和“扩展”两组。
- 内置能力继续负责 Browser、Coding Agent、MCP Apps 的本机配置。
- 扩展区只展示当前设备的 Skills、Plugins、动态 MCP 实时数量，不提供 Skill 文件浏览、Plugin 生命周期、MCP 增删改、环境变量或 OAuth 管理。
- 扩展概要统一读取 Go Runtime 的 `/internal/runtime/overview`；macOS / Windows 不扫描 Skill、Plugin 或 MCP 配置目录，不形成第二套状态模型。
- RuntimeOverview 不可用时只将概要显示为“—”，不影响 Browser / Coding Agent / MCP Apps 的本机配置。
- 完整的 Skill / Plugin / MCP 列表、详情和管理继续由 NexusDock Runtime Web 按节点实时投影 AgentDock Runtime API；Cloud 不复制完整运行时状态到数据库。

这样即使用户不使用 NexusDock，AgentDock 仍能回答“这台设备当前加载了多少扩展”；使用 NexusDock 时，则由统一 Web 控制面承担跨设备观察和管理。

视觉审查：

- Mac / Windows 使用接近的窗口尺寸。
- 五页逐页并排看。
- 首先看信息架构和密度，不陷入像素级细节。
- Settings 必须重点检查首屏信息量、二级导航、滚动长度。
- 不通过截图审查就不要宣布视觉收口。

## 16. 开发规范

遵循 `personal-dev-guard`：

- 长期工程质量优先，不做临时 UI patch 堆叠。
- 可读性优先。
- 不为了架构感制造大量 helper / manager / interface。
- UI 层和平台服务层边界要来自真实职责。
- 不为了测试污染生产 API。
- 核心非显然设计取舍使用中文注释。
- 先看现状，再修改，修改后真实验证。

本轮尤其禁止：

- 为了让 WinUI / SwiftUI 跑起来复制一套业务状态。
- 同一个 Runtime 状态在多个 ViewModel 各自维护。
- 页面直接散落执行 shell / process / filesystem 操作。
- 为了“共享”而抽象跨平台 UI DSL。
- 为了截图硬编码大量页面坐标和一次性 magic number。

## 17. Git / PR 约束

- 原生重构基线 worktree：`/Users/xx/Project/worktrees/agentdock-native-desktop-ui-20261004`
- 当前连接体验 follow-up worktree：`/Users/xx/Project/worktrees/agentdock-native-connection-ux-20261004`
- 当前分支：`refactor/desktop-native-connection-ux-20261004`
- 当前 follow-up 基线：`f5cfad0`
- 原始重构基线：`origin/main@2de2f3d`
- 不在主工作区直接开发。
- 不基于 PR #181 继续叠提交。
- 不整体 cherry-pick PR #181。
- 未经明确要求，不自行 Ready、不 squash merge、不合并。
- 大阶段形成可审查闭环后再提交，不为了保存进度制造碎片 commit。

## 18. 第一阶段完成标准

第一阶段不是“五页都写完”，而是：

1. Mac 新 SwiftUI Shell 可运行。
2. Windows 新 WinUI 3 Shell 可运行。
3. 双端一级导航一致。
4. Home 页面双端实现。
5. Settings 页面双端实现。
6. Settings 二级导航成立。
7. Grouped Section 视觉成立。
8. 双端真机截图可并排审查。
9. 旧系统能力没有因新 Shell 被破坏。
10. 用户确认视觉方向后，再继续 Connections / Capabilities / Activity。

第一阶段最重要的问题只有一个：

> 第一眼看上去，它们是否已经像同一个现代 AgentDock，而不是两个被强行套上相同配色的旧桌面程序？
