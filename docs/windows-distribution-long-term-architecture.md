# Windows 发行与依赖长期架构

> 状态：后续实现设计基线，本文描述目标架构，不代表当前 `main` 已经完成全部改造。
>
> 基线：`origin/main@059e8dc8bcc12015f833a0b76b17c3b99baba69a`（`feat(desktop): decouple cloudflared as optional component (#191)`）。
>
> 本文建立在 `docs/optional-cloudflared-component-architecture.md` 已完成的“cloudflared 从基础安装包解耦”之上，继续收敛 Windows Runtime、第三方组件分发和长期安装器职责。

## 1. 背景

当前 Windows 安装包已经完成一个重要边界调整：`cloudflared` 不再属于基础 Setup payload，而是 AgentDock 管理的 optional component。

但最新 `main` 的 `AgentDockSetup-amd64.exe` 仍约 145 MB。检查当前真实打包配置后，体积主要不再来自 cloudflared，而来自 Windows 原生 UI 的自包含发布模型：

`desktop/windows/winui/AgentDock.WinUI.csproj` 当前包含：

```xml
<WindowsAppSDKSelfContained>true</WindowsAppSDKSelfContained>
<PublishSingleFile>false</PublishSingleFile>
<SelfContained>true</SelfContained>
```

这意味着 Setup 会携带 AgentDock 自身之外的大量 .NET / Windows App SDK runtime 文件。

当前 WSL helper 也是 Setup payload 的一部分，但它不是主要体积来源。按当前 `main` 使用正式构建参数 `-trimpath -ldflags '-s -w'` 实测：

```text
agentdock-wsl-helper-linux-amd64   ~3.2 MB
agentdock-wsl-helper-linux-arm64   ~3.1 MB
```

gzip 后分别约 1.46 MB / 1.31 MB。WSL helper 是 AgentDock 自研、Go 编写、和 AgentDock 协议/版本一起演进的第一方 helper，因此不应为了节省几 MB 引入额外的远程组件生命周期。

长期问题不是“如何继续压缩一个离线大包”，而是重新明确以下责任：

1. AgentDock Installer 应该负责什么；
2. 操作系统/微软官方 Runtime 应该由谁分发；
3. 第三方组件应该从哪里下载；
4. 第一方小型 helper 是否值得远程组件化；
5. R2 应该承载哪些资产；
6. CI 如何保证供应链可审计、可重复、可回滚。

## 2. 最终决策

长期采用下面的发行原则：

1. Windows 只维护一套 AgentDock Setup 逻辑，不维护 Online / Offline 两套安装器代码。
2. Setup 内置 AgentDock 自身必须存在、且和当前版本强绑定的内容。
3. .NET / Windows App Runtime 不再作为 AgentDock 私有 self-contained payload 长期打包，改走 Microsoft 官方支持的 Runtime / Bootstrapper 分发路径。
4. cloudflared 继续是 optional component，但不再由 AgentDock GitHub Release 或 R2 二次托管。
5. cloudflared 客户端直接从 Cloudflare 官方 Release 下载；AgentDock 只维护受审计的 pinned version、官方 URL、格式和 digest。
6. 不做 cloudflared mirror，也不保留 fallback mirror。
7. WSL helper 继续随 AgentDock Windows payload 发布，不上传 R2、不单独下载。
8. R2 只承载 AgentDock 自己拥有发布责任的正式产物、元数据，以及未来确实需要远程分发的第一方大组件。
9. 当前阶段不提供 Offline Installer。未来如果企业场景确实需要离线安装，使用同一套 Installer Engine 和 manifest 自动生成 Offline Bundle，不发展第二套业务逻辑。
10. 不为了“组件化”而把所有二进制都远程组件化；是否独立分发由所有权、体积、版本耦合度和真实使用频率共同决定。

一句话目标：

```text
AgentDockSetup
    ├── AgentDock Core
    ├── Desktop UI / Tray / Arbiter / Shim
    ├── Core Skills
    └── WSL helper（第一方、小、强版本耦合）
            │
            ├── 检测 Microsoft Runtime
            │     └── 缺失 → Microsoft 官方来源
            │
            └── 安装完成

运行时按需能力
    └── cloudflared
          └── Cloudflare 官方 Release
```

## 3. 发行资产职责矩阵

| 资产 | 所有者 | 是否进入 Setup | 下载来源 | 是否进入 R2 | 生命周期 |
| --- | --- | --- | --- | --- | --- |
| AgentDock Core | AgentDock | 是 | Setup / AgentDock Release | 是 | 随 AgentDock |
| WinUI Desktop | AgentDock | 是 | Setup / AgentDock Release | 是 | 随 AgentDock |
| Tray / Arbiter / Shim | AgentDock | 是 | Setup / AgentDock Release | 是 | 随 AgentDock |
| Core Skills | AgentDock | 是 | Setup / AgentDock Release | 是 | 随 AgentDock |
| WSL helper | AgentDock | 是 | Setup | 否 | 随 AgentDock / 协议版本 |
| .NET Runtime | Microsoft | 否 | Microsoft 官方 | 否 | Microsoft |
| Windows App Runtime | Microsoft | 否 | Microsoft 官方 | 否 | Microsoft |
| cloudflared | Cloudflare | 否 | Cloudflare 官方 Release | 否 | AgentDock pin 版本，Cloudflare 提供二进制 |
| 未来大型第一方 optional component | AgentDock | 否 | AgentDock R2 | 是 | 独立组件 |

这里的“进入 R2”是指 AgentDock 自己负责托管二进制。AgentDock 自己的 catalog / latest metadata 属于第一方元数据，可以继续放 GitHub Release / R2；第三方二进制本身不因此被镜像。

## 4. Windows Setup：只维护一个安装产品

### 4.1 不维护两套安装器

不建立：

```text
OnlineInstaller/
OfflineInstaller/
```

也不维护两套依赖检测、事务、回滚和测试。

正式用户入口保持现有风格：

```text
AgentDockSetup-amd64.exe
AgentDockSetup-arm64.exe
```

两个架构可以有各自产物，但必须由同一套 Inno Setup / installer engine、相同依赖模型和相同测试生成。

“在线”不是另一个产品名，而只是安装过程中允许从官方来源满足系统 Runtime 依赖。

### 4.2 Setup 内应该保留的内容

继续随包：

- `agentdock.exe`；
- Native Control Panel；
- Tray；
- Arbiter；
- Shim / Tray Shim；
- icon / install scripts；
- Core Skills；
- WSL helper payload。

这些内容满足至少一个条件：

- AgentDock 自己开发；
- 和当前 AgentDock 版本强绑定；
- 体积相对可控；
- 缺失会直接破坏基础产品能力；
- 独立远程生命周期只会增加复杂度，收益很低。

### 4.3 Setup 不应该继续私有携带的 Runtime

目标从：

```text
AgentDock Setup
  + .NET Runtime
  + Windows App SDK Runtime
```

收敛到：

```text
AgentDock Setup
    ↓
检测 Microsoft Runtime
    ├── 已满足 → 继续
    └── 缺失 → Microsoft 官方 Bootstrap / Runtime 安装机制
```

实现必须遵守当前 Microsoft 对所用 .NET / Windows App SDK 版本的官方部署方式，不自行复制 runtime 文件，也不把 Microsoft Runtime 上传 AgentDock R2。

### 4.4 Runtime 检测与安装约束

后续实现至少满足：

1. 检测必须幂等，已有兼容 Runtime 时不重复安装。
2. 依赖版本使用明确的最低兼容版本或兼容区间，不使用不受控的“latest”语义。
3. 下载必须是 Microsoft 官方 HTTPS 来源。
4. 执行下载的 Runtime 安装器前验证平台签名/官方信任。
5. Runtime 安装失败时明确指出失败的是 Microsoft Runtime，而不是模糊显示 AgentDock 安装失败。
6. AgentDock 主安装事务不能在 Runtime 失败后留下半激活 generation。
7. CI 必须同时覆盖“机器已有 Runtime”和“Runtime 缺失，需要 bootstrap”两种场景。
8. 实现时以开发当日 Microsoft 官方文档为准，不硬编码本文未验证的具体 bootstrapper CLI 参数。

### 4.5 WinUI 发布目标

`AgentDock.WinUI.csproj` 应从当前完全 self-contained 模型转成适合当前 WinUI 版本的 framework-dependent / Microsoft 官方 Runtime 模型。

目标不是为了得到某个固定 MB 数，而是去掉重复携带系统级共享 Runtime 的长期责任。

不要为缩包优先采用高风险 aggressive trimming、手工删除 Windows App SDK 文件或其他脆弱裁剪方式。先把 deployment model 做正确，再测最终体积。

实现阶段进一步确认 Windows App SDK 元包会无条件引入 AgentDock 未使用的 AI / ML / Widgets 等组件。最终发行基线固定到 Microsoft 官方下载页公开支持的稳定 `2.1.3`，并显式引用该元包对应的最低组件版本：`WinUI 2.1.0`、`Runtime 2.1.3`、`Foundation 2.0.21` 与 `InteractiveExperiences 2.0.13`。Runtime bootstrap 使用 Microsoft 文档给出的版本化 `aka.ms/windowsappsdk/2.1/2.1.3/...` 下载地址，并在执行前继续做 Authenticode Microsoft 签名校验。这是从 NuGet 依赖源头收窄功能面，不是安装后手工删除文件；两种架构都必须继续验证 bootstrap DLL、WinUI 资源和 framework-dependent 启动链完整。

## 5. WSL helper：继续随包

### 5.1 为什么不迁 R2

WSL helper 当前：

- 是 AgentDock 自己开发；
- 源码位于 `cmd/agentdock-wsl-helper` 和 `internal/wslfilehelper`；
- 使用 Go 构建；
- 单架构只有约 3 MB；
- 和 AgentDock 的 WSL 文件协议共同演进；
- 当前已有 `wsl-helper/manifest.json` 管理 protocol version 和各架构 digest；
- 首次调用 WSL 能力时会部署到 WSL 用户目录。

为了减少约 2～3 MB 的压缩体积，把它改成：

```text
Setup → R2 → helper catalog → 下载 → 缓存 → 部署
```

会引入额外网络失败、版本错配、缓存清理和独立发布问题，长期收益不成立。

因此保留当前原则：

```text
AgentDock Windows payload
└── wsl-helper/
    ├── manifest.json
    ├── agentdock-wsl-helper-linux-amd64
    └── agentdock-wsl-helper-linux-arm64
```

### 5.2 后续允许优化，但不作为本轮目标

如果未来有明确证据表明 Windows host architecture 可以安全决定唯一 WSL architecture，可以再评估每个 Setup 只携带一个 helper。

在没有覆盖 WSL on ARM、兼容发行版和升级路径的真实验证前，不为了几 MB 提前改变当前双架构 payload。

## 6. cloudflared：官方上游直连，不做 mirror

### 6.1 当前实现需要继续收敛的地方

`#191` 已经把 cloudflared 从基础 Setup / DMG 中移除，但当前 Release pipeline 仍然会：

```text
Cloudflare GitHub Release
    ↓
AgentDock CI 下载
    ↓
AgentDock GitHub Release 再发布一份
    ↓
R2 再镜像一份
    ↓
AgentDock catalog 指向自己的副本
```

这属于“托管生命周期已经解耦，但分发仍二次 vendoring”。

长期目标明确取消这层二次托管。

### 6.2 目标链路

```text
AgentDock 仓库中的 pinned component metadata
    ↓
Release CI 验证 Cloudflare 官方资产
    ↓
AgentDock component catalog
    ↓
artifact.url = Cloudflare 官方不可变版本 URL
    ↓
客户端直接从 Cloudflare 官方下载
    ↓
AgentDock 校验 digest / version / platform trust
    ↓
AgentDock component store
```

不经过：

- AgentDock GitHub Release cloudflared asset；
- AgentDock R2 cloudflared asset；
- fallback mirror。

### 6.3 不允许直接消费 latest

AgentDock 不得运行时读取 Cloudflare “latest” 并自动选择版本。

版本升级必须通过仓库中的显式变更：

```text
cloudflared version
official asset URL
asset format
SHA-256
platform / arch
```

变更进入正常 PR / CI 审查后，才成为 AgentDock 认可的新版本。

这样 upstream 即使新发版本，也不会绕过 AgentDock 的发布审核直接影响现有用户。

### 6.4 建议增加第一方 pinned metadata

不要让 Release CI 每次下载 upstream 后“现场计算一个新 digest 再信任它”，否则 upstream 资产如果被替换，CI 只会接受新的字节。

建议建立一个小而明确的第一方 metadata 文件，例如：

```text
packaging/components/cloudflared.json
```

逻辑字段：

```json
{
  "component": "cloudflared",
  "version": "2026.x.y",
  "upstream_source": "https://github.com/cloudflare/cloudflared/releases/tag/2026.x.y",
  "artifacts": [
    {
      "os": "windows",
      "arch": "amd64",
      "format": "binary",
      "url": "https://github.com/cloudflare/cloudflared/releases/download/2026.x.y/cloudflared-windows-amd64.exe",
      "sha256": "..."
    },
    {
      "os": "darwin",
      "arch": "arm64",
      "format": "tgz",
      "url": "https://github.com/cloudflare/cloudflared/releases/download/2026.x.y/cloudflared-darwin-arm64.tgz",
      "sha256": "..."
    }
  ]
}
```

这里只是设计形状；实现时可以沿用当前 Go struct，只要契约清楚，不需要为了 metadata 再设计复杂通用 package。

### 6.5 macOS 官方资产格式需要显式处理

Windows 上游直接提供 `.exe`。

当前 macOS Release CI 从 Cloudflare 官方下载 `.tgz`，解出 `cloudflared` 后才重新发布 raw binary。取消镜像以后，客户端不能继续假设所有 artifact 都是裸二进制。

因此 component catalog / installer 需要明确 artifact format：

- Windows：`binary`；
- macOS：`tgz`。

macOS 下载流程必须：

1. 下载固定版本官方 tgz；
2. 对下载的 tgz 做 pinned SHA-256 校验；
3. 在受控临时目录解包；
4. 只接受预期的单个普通文件 `cloudflared`；
5. 拒绝 symlink、path traversal、额外不可信目标路径；
6. 验证平台签名/信任；
7. 执行 `cloudflared --version` 并与 catalog 版本一致；
8. 计算最终安装二进制 digest；
9. 写入版本目录并原子更新 `active.json`。

下载资产 digest 和安装后二进制 digest 是两个不同语义：

- catalog digest 保护 upstream 下载内容；
- `active.json` digest 保护本地已安装二进制。

不要继续把二者强行当成同一个值。

### 6.6 Release CI 对 cloudflared 的职责

Release CI 应从“构建/上传 cloudflared component asset”改成“验证 upstream dependency”。

至少验证：

- pinned URL 使用 HTTPS；
- URL 是固定版本，不是 latest；
- 下载内容与仓库 pinned SHA-256 一致；
- Windows Authenticode 有效；
- macOS 平台信任验证通过；
- `cloudflared --version` 与 metadata 一致；
- catalog 中三个现有平台/架构映射完整；
- catalog 的 URL 仍然指向 Cloudflare 官方来源。

验证完成后只生成 AgentDock 自己的 component catalog，不上传 cloudflared 二进制。

## 7. Component Catalog 的长期边界

Component catalog 是 AgentDock 自己的元数据，因此可以继续作为 AgentDock Release asset，也可以跟随 AgentDock 的 R2 最新版元数据分发。

但 catalog 内不同组件的下载来源由所有权决定：

```text
third-party component
    → 官方 upstream URL

first-party remote component
    → AgentDock R2 URL
```

R2 mirror 阶段不允许无差别改写所有 `artifact.url`。

当前 `tools/release/mirror.go` 会把 component catalog 重写到 R2 versioned base；后续必须改成只重写 AgentDock 自有资产，第三方 official URL 保持原样。

不要因为 catalog 位于 R2，就把第三方 binary 也迁入 R2。

## 8. R2 长期职责

R2 负责 AgentDock 自己控制的下载体验，不做第三方通用镜像仓库。

### 8.1 应该进入 R2

- `AgentDockSetup-*.exe`；
- `AgentDock-macos-universal.dmg`；
- AgentDock CLI / archive；
- AgentDock install scripts；
- checksums；
- `latest.json`；
- AgentDock component catalog；
- 未来真正独立发布、体积较大或生命周期独立的第一方 optional component。

### 8.2 不应该进入 R2

- cloudflared；
- Microsoft .NET Runtime；
- Windows App Runtime；
- 其他可以从可信官方 upstream 直接获得、且 AgentDock 不拥有发布责任的第三方二进制。

### 8.3 第一方 component 何时值得放 R2

同时满足若干条件再做：

- 体积明显；
- 大量用户不会使用；
- 生命周期可以独立于主 AgentDock；
- 可以单独升级而不破坏协议；
- 随主包携带会显著增加下载成本。

WSL helper 当前不满足这些条件。

## 9. 安装、升级与回滚语义

### 9.1 AgentDock 主程序

AgentDock 主程序继续使用当前 generation / transactional upgrade 模型。

Runtime dependency bootstrap 发生在真正激活新 generation 前，并且失败不能污染 current pointer。

### 9.2 Microsoft Runtime

Microsoft Runtime 是系统级依赖，不纳入 AgentDock generation rollback。

AgentDock 只需要保证：

- 安装前依赖满足；
- 安装失败能给出明确诊断；
- 不擅自删除系统 Runtime；
- uninstall AgentDock 时不卸载可能被其他应用共享的 Microsoft Runtime。

### 9.3 cloudflared

继续使用独立 component store：

```text
components/cloudflared/
├── active.json
└── versions/
    └── <version>/
```

AgentDock 升级不应该自动删除或覆盖已安装 cloudflared。

AgentDock component update 只更新 cloudflared，不升级 AgentDock。

### 9.4 WSL helper

WSL helper 属于 AgentDock generation payload。AgentDock 升级时随 generation 一起更新，实际部署到 WSL 时继续使用 manifest / protocol version 判断和替换。

## 10. 发布物命名与用户体验

普通用户不需要理解 Runtime、component、mirror 等内部概念。

Release / 官网主入口保持简单：

```text
Windows
  AgentDockSetup-amd64.exe
  AgentDockSetup-arm64.exe

macOS
  AgentDock-macos-universal.dmg
```

不新增：

```text
Online
Offline
Full
Bootstrapper
Runtime
Portable
```

等普通用户难以判断的并行下载入口。

cloudflared 也不应该作为 GitHub Release 的普通用户附件继续出现。

## 11. Offline Installer 决策

当前阶段明确不做 Offline Installer。

原因：

- AgentDock 核心产品本身面向联网环境；
- NexusDock / Cloudflare Tunnel / 更新等能力本身依赖网络；
- 维护离线完整依赖会再次把第三方 runtime 和 component 全塞回包里；
- 当前没有足够真实企业离线需求证明这项复杂度合理。

未来若出现真实需求，只允许：

```text
同一 Installer Engine
+ 同一 dependency/component manifest
+ CI 预取的离线 payload
= Offline Bundle
```

不允许发展第二套安装器业务逻辑。

## 12. 建议实施阶段

### Phase 1：WinUI Runtime 解耦

目标：解决 Windows Setup 体积的主要来源。

任务：

1. 调整 `desktop/windows/winui/AgentDock.WinUI.csproj` 发布模型；
2. 去掉 AgentDock 私有 self-contained .NET / Windows App Runtime；
3. 在 Setup 中增加 Runtime 检测；
4. 缺失时走 Microsoft 官方支持的安装方式；
5. 保持现有 AgentDock generation transaction；
6. 增加 runtime-present / runtime-missing E2E；
7. 重新测量 Setup 体积和安装目录体积。

不要顺手改 cloudflared / WSL helper 业务。

### Phase 2：cloudflared 去二次托管

目标：只保留官方 upstream + AgentDock pinned metadata。

任务：

1. 引入/整理 pinned cloudflared metadata；
2. component catalog 改为官方 URL；
3. macOS component install 支持官方 tgz；
4. catalog digest 语义调整为 download artifact digest；
5. active pointer 继续记录 installed binary digest；
6. Release CI 改为 upstream verify，不再 upload cloudflared；
7. R2 mirror 不再上传 cloudflared；
8. R2 catalog rewrite 不改第三方 URL；
9. 删除 GitHub Release / R2 manifest 中 cloudflared required asset；
10. 更新 `docs/release-distribution.md`。

### Phase 3：发布链与回归收口

目标：确认架构不会在旧脚本、self-update、migration 中重新耦合。

检查：

- Windows Setup payload；
- macOS App Bundle；
- install.ps1 / install.sh；
- self-update；
- release verification；
- R2 mirror prepare / manifest；
- component install/update/uninstall；
- legacy bundled cloudflared migration；
- Windows amd64 / arm64；
- macOS amd64 / arm64；
- WSL helper deploy。

## 13. 预计涉及代码

开发前必须再次以最新 `main` 为准，但当前基线重点文件包括：

### Windows Runtime / Setup

- `desktop/windows/winui/AgentDock.WinUI.csproj`
- `packaging/windows/AgentDock.iss`
- `packaging/windows/includes/code.iss`
- `packaging/windows/build-windows-setup.ps1`
- `.github/workflows/windows-installer.yml`
- `.github/workflows/release.yml`

### cloudflared component

- `internal/component/cloudflared.go`
- `internal/component/cloudflared_test.go`
- `internal/component/trust_windows.go`
- `internal/component/trust_darwin.go`
- `tools/release/component_catalog.go`
- `tools/release/mirror.go`
- `tools/release/mirror_test.go`
- `.github/workflows/release.yml`

### WSL helper

WSL helper 本轮原则上不修改，只需要防止误拆：

- `cmd/agentdock-wsl-helper/`
- `internal/wslfilehelper/`
- `packaging/windows/build-wsl-helper-payload.ps1`
- `internal/tool/file/wsl_helper_runtime_windows.go`

### 文档

- `docs/release-distribution.md`
- `docs/optional-cloudflared-component-architecture.md`
- 本文

## 14. CI 与供应链验收

完成实现后至少满足：

### Windows Setup

- Setup 不包含 cloudflared；
- WinUI 不再私有携带完整目标 Microsoft Runtime；
- 新机缺 Runtime 时可通过官方来源完成依赖满足；
- 已有 Runtime 时不重复安装；
- AgentDock 安装/升级/卸载 E2E 通过；
- Windows amd64 / arm64 正确；
- Setup 签名保持有效；
- 安装失败不会留下错误 current generation。

### cloudflared

- GitHub AgentDock Release 不再包含 cloudflared binary；
- R2 不再包含 cloudflared binary；
- component catalog 指向 Cloudflare 官方固定版本 URL；
- digest 是仓库审计过的 pinned digest；
- Windows Authenticode 验证通过；
- macOS archive 安全解包与平台信任验证通过；
- install / update / repair / uninstall 回归通过；
- Tunnel quick / named 生命周期继续通过；
- legacy bundled cloudflared migration 继续可用。

### WSL helper

- 仍随 Windows payload；
- manifest digest 与实际 helper 一致；
- linux/amd64 和 linux/arm64 都存在；
- WSL deploy E2E 继续通过；
- 不新增 R2 网络依赖。

### R2

- 只上传第一方资产和第一方 metadata；
- mirror manifest 不再要求 cloudflared；
- component catalog 中 third-party official URL 不被 rewrite；
- latest.json 仍只在所有第一方 versioned asset 验证后更新。

## 15. 测量与成功标准

不要把“安装包必须低于某个固定数字”作为架构门禁。

实现 Phase 1 前后都记录：

- `AgentDockSetup-amd64.exe`；
- `AgentDockSetup-arm64.exe`；
- 解包后的 AgentDock payload；
- WinUI control-panel 目录；
- WSL helper；
- AgentDock Core / helpers；
- 首次安装需要额外从 Microsoft 下载的字节数。

主要成功标准：

1. Setup 明显减少重复 Runtime 负担；
2. 依赖责任清楚；
3. 第三方二进制不再由 AgentDock 二次托管；
4. 第一方小 helper 不因过度组件化增加复杂度；
5. 一套 installer logic 可以长期维护；
6. 任何 optional component 失败不影响普通 NexusDock 用户安装 AgentDock。

## 16. 明确不做

本轮不要：

- 为了体积把 WSL helper 拆到 R2；
- 建 Online / Offline 两套安装器；
- 建 cloudflared mirror / fallback mirror；
- 把 Microsoft Runtime 上传 R2；
- 自动追踪 Cloudflare latest；
- 为单一 cloudflared 提前建设通用第三方插件市场；
- 通过手工删除 WinUI runtime 文件做脆弱瘦身；
- 把 cloudflared 再塞回 Setup 或 macOS App Bundle；
- 为了缩包破坏当前 generation rollback / Windows E2E。

## 17. 开发原则

实现时遵循以下顺序：

1. 先确认最新 `main` 与当前微软官方部署要求；
2. 先做 Runtime 正确的 deployment model，再优化体积；
3. cloudflared 先保证供应链 pin 和官方直连，再删除旧 mirror；
4. 每一个删除旧 Release asset 的改动都同时更新 catalog、R2 manifest、CI 和文档；
5. 保持现有 component store / active pointer 的简单边界，不引入不必要的通用抽象；
6. 修改前看真实现状，修改后跑真实跨平台门禁；
7. Windows Installer 属于高风险发布链，不以“本地能编译”代替安装/升级/卸载 E2E。

## 18. 最终目标形态

```text
                         ┌─────────────────────────┐
                         │ Microsoft official CDN  │
                         │ .NET / Windows Runtime  │
                         └────────────┬────────────┘
                                      │
                                      ▼
┌───────────────────────┐     ┌───────────────────────┐
│ AgentDock R2 / Release│────▶│ AgentDockSetup        │
│ first-party only      │     │ one installer logic   │
└───────────────────────┘     └───────────┬───────────┘
                                          │
                    ┌─────────────────────┼──────────────────────┐
                    ▼                     ▼                      ▼
             AgentDock Core        Native Desktop          WSL helper
                                                      first-party / bundled

                         Runtime optional component
                                      │
                                      ▼
                         ┌─────────────────────────┐
                         │ Cloudflare official     │
                         │ pinned cloudflared URL  │
                         └────────────┬────────────┘
                                      │
                                      ▼
                         AgentDock component store
```

最终边界是：

> AgentDock 只托管和维护自己真正拥有的发行物；系统 Runtime 回到系统供应商，第三方组件回到第三方官方上游，小型强耦合第一方 helper 留在主发行包。这样安装器体积、供应链责任和后续维护复杂度同时收敛。
