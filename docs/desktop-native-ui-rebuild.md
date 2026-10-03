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
| 默认窗口 | 1060 × 720 |
| 最小窗口 | 900 × 620 |
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

建议结构：

```text
活动

Runtime

┌────────────────────────────────────────────┐
│ Runtime                          正在运行 ●│
├────────────────────────────────────────────┤
│ 启动时间                        23:41       │
├────────────────────────────────────────────┤
│ 本地地址                        127.0...    │
└────────────────────────────────────────────┘

最近活动

00:17  MCP 客户端已连接
00:12  Browser Skill 已调用
23:58  NexusDock 连接恢复

日志

日志目录                              打开 >
配置目录                              打开 >
```

底部可提供轻量“最后刷新 / 刷新”状态，不做 Dashboard Status Cards。

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

- TianYi 真机 `dotnet publish`。
- 真机启动。
- 五页 UI 截图。
- Runtime start / stop。
- 单实例 / 托盘 / 启动行为。
- 安装器兼容性。

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

- 本轮开发 worktree：`/Users/xx/Project/worktrees/agentdock-native-desktop-ui-20261004`
- 本轮分支：`refactor/desktop-native-ui-20261004`
- 基线：`origin/main@2de2f3d`
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
