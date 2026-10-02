# AgentDock Desktop 控制面板设计规范

> 状态：2026-10-02 第一版重构基线。本文是 macOS / Windows 控制面板的信息架构、视觉语义和组件约束，后续新增桌面 UI 应优先遵循这里，而不是在单个平台单独设计。

## 1. 产品定位

AgentDock Desktop 是本机 AI Runtime 的运行中心，不是 NexusDock SaaS 的桌面壳，也不是把所有配置一次性铺开的系统设置页。

用户打开控制面板首先应回答三个问题：

1. AgentDock 现在是否正常。
2. 当前有哪些连接和能力可用。
3. 出现问题时下一步可以做什么。

技术参数、端口、Token、Tunnel Token、ACP 路径等只在用户主动进入配置时出现。

AgentDock 作为开源项目必须在不连接 NexusDock 的情况下完整成立。NexusDock 仅作为 Remote Access 的托管连接方式之一，不在首页展示订阅、套餐、升级或营销信息。

## 2. 一级信息架构

桌面控制面板统一使用轻量 App Shell：

- **Home**：运行状态、关键能力摘要、常用动作。
- **Connections**：AI 客户端连接与 Remote Access。
- **Capabilities**：Browser、Coding Agent、MCP 等本机能力。
- **Activity**：运行状态、诊断、日志和近期可观察信息。
- **Settings**：通用设置、Runtime、权限、启动项和高级配置。

不把 Logs、Diagnostics、NexusDock、Browser 等继续提升为一级导航，避免侧栏变成功能目录。

## 3. Connections 与 Remote Access

Connections 负责“AI 如何连接 AgentDock”，Capabilities 负责“连接以后 AgentDock 能做什么”。

Remote Access 的连接方式包括：

- 临时域名：快速测试，地址可以变化。
- 固定域名：用户自己维护的稳定公网地址。
- NexusDock：托管式远程连接。

默认页面只展示当前状态和当前连接；“临时域名 / 固定域名 / NexusDock”不在同一层一次性铺成三张营销卡。用户点击“设置 / 更改连接”后才进入紧凑的连接方式选择。

底层如果允许多种连接并存，UI 不应强行建模为永远互斥的三选一。

## 4. 视觉原则

整体参考现代原生桌面工具的轻量工作台风格，但不一比一复刻任何产品：

- 内容区使用浅层级背景，卡片只承担信息分组，不滥用卡片嵌套。
- 主状态使用明确的标题、状态色和动作，不用大段表单充当首页。
- 一级导航稳定，不根据连接状态动态消失。
- 未配置 Remote Access 是正常状态，不使用告警色。
- 信息密度适中，默认视图避免同时出现端口、Token、Endpoint、Tunnel Token 等技术细节。
- macOS 与 Windows 统一信息架构、组件语义、间距层级和状态含义；具体控件遵循各自平台习惯。

## 5. Design System

### 5.1 Token 语义

Windows 当前基线：

- Accent：#1769AA
- Window：#F6F8FB
- Surface：#FFFFFF
- Muted Surface：#F9FAFC
- Border：#E1E6EE
- Primary Text：#172033
- Secondary Text：#667085

macOS 使用对应的系统动态色，确保 Light / Dark Mode 和可访问性跟随系统；不硬编码 Windows 颜色值。

### 5.2 组件语义

两端保持同一套组件概念：

- AppShell
- SidebarItem
- PageHeader
- StatusHero
- SectionCard
- StatusBadge
- FieldRow
- ToggleRow
- PrimaryButton
- SecondaryButton
- QuietButton
- InlineMessage

这些是产品语义，不要求两端共享实现文件。

### 5.3 图标

- macOS：SF Symbols。
- Windows：Segoe Fluent Icons / Fluent System Icons。
- 品牌 Logo 与第三方品牌标识使用项目资产。
- 页面代码按语义选择图标，不使用 Emoji、随意 Unicode 或每页单独绘制 SVG。

同一个语义在两端可以使用符合平台习惯的不同图形，例如 Settings 在 macOS 用 SF Symbol，在 Windows 用 Fluent glyph。

## 6. 平台实现边界

### macOS

保留 AppKit。主窗口升级为轻量 Sidebar + Content Shell；复杂表单仍可保留独立高级设置窗口。优先使用 NSStackView、NSBox、NSScrollView、NSSplitView 等原生控件和 SF Symbols。

### Windows

保留 WPF。通过 ResourceDictionary / Style / ControlTemplate 实现 Token 与组件层，主窗口使用左侧导航 + 内容区域。不得为了这次视觉重构引入 Electron、Tauri、Avalonia、WinUI 3 或大型 WPF 组件库。

## 7. 当前重构范围

本轮必须完成：

- 统一 macOS / Windows 的视觉语言与导航语义。
- 引入 Home / Connections / Capabilities / Activity / Settings 的 App Shell。
- 把现有 Runtime、Tunnel、Nexus、Browser、Coding Agent、MCP、日志、权限、启动项等功能迁移到合适的信息层级。
- 保留现有行为和协议，不修改 Core / MCP / Nexus 连接协议。
- 补充与新 UI 结构相关的构建和回归测试。

本轮不做：

- Workflow / Recall / Plugin 市场等新的产品功能。
- NexusDock 订阅、套餐、支付或营销入口。
- 为“更像产品”而制造假的 Activity 数据。
- 跨平台 UI 框架迁移。

## 8. 演进条件

当桌面端出现大量表格、搜索、过滤、Command Palette、复杂导航、可插拔页面、虚拟列表等真实需求时，再重新评估 WinUI 3 / Avalonia 等框架。当前规模优先保持原生技术栈和轻量 Design System，避免为了未来假设提前付出迁移成本。
