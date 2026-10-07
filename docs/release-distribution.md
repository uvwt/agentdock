# Release 下载分发

GitHub Release 的 Assets 是面向人工下载的精简视图，不等同于完整 Distribution Catalog。正式 Release 只展示 Windows Setup、macOS DMG、Linux amd64/arm64 归档和统一的 `SHA256SUMS.txt`；macOS updater ZIP、Windows payload ZIP、Darwin CLI 归档、bootstrap、component catalog 及机器 sidecar checksum 只保留在 R2 的版本化 Release 快照中。GitHub 与 R2 公共资产必须来自同一份已经通过 gate 的 candidate 字节，禁止为不同分发渠道重复构建。

AgentDock 的官方第一方二进制分发边界统一为 `https://download.nexusdock.co`。桌面更新、安装脚本、AgentDock 归档、Setup/DMG、校验文件和第一方 component metadata 不依赖 AgentDock GitHub Release 下载地址；GitHub Release 继续承担 Release Notes、社区入口和面向人工下载的长期历史归档职责；完整机器分发契约由 R2 承担。

第三方依赖默认不进入 AgentDock 镜像链。`cloudflared` 是一个有意的例外：其 URL、格式和 SHA-256 仍由 AgentDock 审计过的 component catalog 固定，独立的 `Publish cloudflared component` workflow 从 Cloudflare 官方固定 Release 下载并验证，再把**完全相同的字节**发布到 R2 的不可变 component mirror。客户端优先 R2，只有镜像网络失败时才回退 Cloudflare 官方 GitHub Release；无论来源都使用同一个 upstream SHA-256 和平台信任验证。Microsoft .NET Windows Desktop Runtime 与 Windows App Runtime 仍使用固定的微软官方来源，不做 R2 镜像。

## 产品契约

普通用户只有一个公开 Stable 更新通道：

- `install.sh` 不提供历史版本选择；
- `install.ps1` 不提供历史版本选择；
- 桌面 self-update 只读取 `https://download.nexusdock.co/latest.json`；
- Stable 客户端的 component resolver 读取 `https://download.nexusdock.co/components/v1/catalog.json`；
- 客户端运行时不使用 AgentDock GitHub Release fallback；
- GitHub 历史 Release 可用于开发、排障和人工归档下载，但不属于产品安装 API。

桌面 self-update 只负责发现、下载和验证完整平台安装制品，不再承担第二套安装器职责：

- Windows 托管安装下载对应架构的 `AgentDockSetup-<arch>.exe` 与 checksum，校验 SHA-256 和 Authenticode 后把更新交给 Setup；运行时依赖、generation、Tray/WinUI 文件、启动项、配置保留和回滚只由 Setup / Installer Engine 管理。
- macOS 下载 `AgentDock-macos-universal.zip`，只验证 AgentDock Bundle 身份、目标版本和整个 App 的代码签名，然后事务式整体替换 `AgentDock.app`；updater 不检查 `Contents` 内具体 Helper、Framework、component 或 Skill 文件。
- 包内资源初始化由目标版本应用自己完成。只有持久化用户数据 schema、凭据格式或 OS 注册契约发生变化时才允许新增 migration；程序包内部文件增删不构成 updater migration。
- Release gate 必须覆盖上一 Stable 到候选版本的真实平台升级路径，不能用“某个内部文件仍然存在”代替升级兼容性验证。

### v0.9.x 一次性升级桥

`v0.9.1` 已发布客户端把 GitHub `releases/latest` 固化成更新 API，而且旧 updater 会解析 Windows ZIP / macOS App 的内部文件。这个已经发布的协议不能由 `v1.x` 代码回头修改。因此 `v1.0.0` 迁移期使用一次性的 `v0.9.2` compatibility bridge：

1. `v0.9.1` 继续按它已经发布的旧协议，从 GitHub Latest 更新到包结构兼容的 `v0.9.2`；
2. `v0.9.2` 只 backport 新 updater 协议：Windows 交给完整 Setup，macOS 整体替换 App；
3. `v1.x` 和后续版本只发布新协议，不继续携带旧 WPF / bundled cloudflared 等兼容文件；
4. 兼容窗口内仓库变量 `LEGACY_GITHUB_LATEST_TAG` 固定 GitHub Latest 到 bridge。当前 Stable 仍由 R2 `latest.json` 唯一发布，二者职责分离；
5. 当 `v0.9.1` 超出明确支持窗口后删除该仓库变量，即恢复 GitHub Latest 跟随当前 Stable。

bridge 是旧协议到新协议的单向入口，不允许在 `v1.x` updater 中继续新增版本号特判。

内部发布验证使用标准 SemVer prerelease，例如 `v1.0.0-rc.1`。Prerelease 不是第二个长期产品通道：它只发布不可变候选产物和 GitHub Prerelease，不移动 `latest.json`、component repository 或容器 `latest` aliases。RC 的 self-update 仍检查 Stable `latest.json`，因此 `1.0.0-rc.1 < 1.0.0` 时会自然升级正式版。

## R2 对象模型

`releases/<tag>/` 是不可变发布快照；大型 Release 资产采用短 retention。component repository 是独立的长期兼容性 metadata：

```text
latest.json                         # 当前 Stable，可变
components/
├── v1/
│   └── catalog.json               # component v1 兼容仓库，可变且 revision 单调递增
└── cloudflared/
    └── 2026.9.1/                  # 第三方原始字节不可变镜像，不按 AgentDock Release 复制
        ├── cloudflared-windows-amd64.exe
        ├── cloudflared-darwin-amd64.tgz
        ├── cloudflared-darwin-arm64.tgz
        ├── cloudflared-linux-amd64
        ├── cloudflared-linux-arm64
        └── LICENSE
releases/
├── v1.0.0-rc.1/                   # 内部 RC 验证期间保留
│   ├── agentdock-component-catalog.json
│   └── ...
├── v0.9.1/                        # 上一 Stable
│   └── ...
└── v1.0.0/                        # 当前 Stable
    ├── AgentDockSetup-amd64.exe
    ├── AgentDock-macos-universal.dmg
    ├── agentdock-component-catalog.json
    ├── agentdock_{linux,darwin,windows}_{amd64,arm64}.*
    ├── install.sh
    └── install.ps1
```

同一个 `releases/<tag>/` key 不允许以不同 SHA-256 覆盖。每个 Release 中的 `agentdock-component-catalog.json` 是该候选构建时的审计快照；Stable 客户端运行时不依赖历史 Release snapshot，因此历史大文件被清理后不会失去安装 optional component 的能力。

`latest.json` 使用最小 GitHub Release 兼容结构：`tag_name` 和 `assets[].name/browser_download_url`，所有资产 URL 指向不可变 `releases/<tag>/...`。Download Worker 只处理 `/latest/*` 的友好入口；`/releases/*` 和 `/components/*` 由 R2 自定义域直接提供。

Android 使用独立的 `android/latest.json` / `android/releases/` 契约。

## Component repository

组件仓库源文件是 `internal/component/catalog-v1.json`。Schema v2 的稳定语义包括：

- `revision`：任何 metadata 变化都必须单调递增，用于客户端回滚保护；
- `component` / `version`：组件身份和 SemVer；
- `status`：`supported`、`deprecated` 或 `revoked`；
- `agentdock.min_version` / `max_version_exclusive`：AgentDock 兼容范围；
- 固定的 upstream version/source；
- 各平台 artifact format、官方 `url`、不可变 R2 `mirror_url` 和 upstream SHA-256。

客户端优先选择“兼容、未撤销、版本最高”的 `supported` 条目；没有 supported 时才允许使用兼容的 `deprecated` 条目，`revoked` 永远不可选。

Stable 默认从 `/components/v1/catalog.json` 获取。Prerelease 默认从自己的 `/releases/<prerelease-tag>/agentdock-component-catalog.json` 获取，因此 RC 可以验证精确候选 metadata 而不提前改变 Stable 用户契约。正式版通过全部 gate 后，再把同一份已验证 snapshot 提升到 component repository。

客户端容错顺序是：

1. 远程 catalog；
2. 本地 last-known-good catalog；
3. 随当前 AgentDock 二进制嵌入的 baseline catalog。

随当前 AgentDock 二进制嵌入的 baseline revision 是生产 resolver 的最低可信版本：远程或缓存 catalog 不能低于它，相同 revision 也必须与 baseline 内容一致。成功获取的更高 revision 远程 catalog 会原子缓存；若之后远程 `revision` 小于本地已验证 revision，或相同 revision 却内容不同，客户端继续保留 last-known-good，避免服务端回滚、CDN 旧缓存或 metadata 污染覆盖已经获知的兼容性/撤销状态。显式 `--catalog-url` 属于测试/受管覆盖，按权威源 fail-closed，不使用生产 fallback。

baseline 只解决控制面 metadata 暂时不可达，不降低 artifact 信任要求：cloudflared 的官方 URL 与镜像 URL 都必须匹配固定版本路径，下载内容必须通过同一个 upstream SHA-256 和平台签名/代码签名验证。镜像返回成功但 digest 不一致时 fail-closed，不回退官方源掩盖镜像异常。

## 版本与平台 metadata

产品版本遵循完整 SemVer，例如：

`1.0.0-beta.1 < 1.0.0-rc.1 < 1.0.0 < 1.1.0-rc.1`

self-update 不再截掉 prerelease 后缀比较版本，因此 prerelease 可以升级正式版，正式版不会被错误降级到 prerelease。

Windows 的数值型 FileVersion / AssemblyVersion 使用 SemVer core（例如 `1.0.0.0`），InformationalVersion 与 AgentDock 产品版本保留完整 `1.0.0-rc.1`。Inno Setup 的展示版本也保留完整产品版本。

macOS 的 `CFBundleShortVersionString` 使用 SemVer core，`CFBundleVersion` 使用 Release CI 的单调 build number，完整产品版本写入 `AgentDockReleaseVersion`，应用 UI 和 Core 一致性检查读取该字段。

正式发布版本以不可变 Git Tag 为唯一事实来源。仓库源码中的 `buildinfo.Version` 只保留 `0.0.0-dev` 开发身份；Release CI 校验 Tag 是合法完整 SemVer 后，把去掉 `v` 前缀的版本通过 Go linker 注入所有正式二进制，并把同一版本继续派生到 Windows/macOS metadata。普通源码构建不会冒充某个已发布版本。

## R2 保留策略

Stable 正常收敛到两个完整版本：

1. 新发布的当前 Stable；
2. R2 `releases/` 中低于当前版本的最高合法 Stable，也就是实际仍存储的上一 Stable。

`latest.json` 是客户端更新入口，不再作为 retention inventory。这样即使一次性 compatibility bridge 仍被旧客户端引用、但对应 versioned R2 prefix 已按短 retention 清理，也不会阻断后续版本回收。

RC 在候选验证期间保留其不可变 `releases/<tag>/`。正式 Stable promotion 时，release tool 基于实际 R2 prefix 和统一 SemVer 规则生成 retention plan：保留当前 Stable 与上一 Stable，清理更旧且能够识别的 SemVer release prefix，包括已完成使命的 prerelease。未知 prefix 和高于当前发布版本的 prefix 一律保留，避免旧 tag 重跑或异常对象导致误删；如果没有更早 Stable，则只保留当前 Stable。

GitHub Releases 始终保存完整历史，R2 不承担长期历史安装仓库职责。

`components/v1/catalog.json` 不跟随 Release retention 删除。它按 component API 生命周期维护；只要仍支持使用 v1 catalog 的客户端，就持续提供兼容 metadata。`components/cloudflared/<version>/` 同样不参与 AgentDock Release retention；它按 component compatibility 生命周期保留。通常多个 AgentDock 版本共享同一套 cloudflared 版本，因此不会为每个 AgentDock Release 复制第三方二进制。

## Bootstrap 约定

源码仓库和正式 Release 中的 `install.sh` / `install.ps1` 只通过 `https://download.nexusdock.co/latest` 获取 AgentDock 第一方 payload；bootstrap 自身不再维护第三方 cloudflared 版本或下载 URL。需要 Quick/Named Tunnel 时，安装流程把依赖解析交给 AgentDock component resolver，由它读取 component catalog、优先使用 R2 immutable mirror，并只在镜像请求失败时回退审计过的 Cloudflare 官方固定 Release。

Release 打包阶段不改写 bootstrap 为版本化下载地址。GitHub Release 与 R2 发布同一份 bootstrap 字节及 checksum。测试和离线验证可以注入本地 AgentDock payload 或显式 cloudflared binary，但不构成面向用户的历史版本安装功能，也不会恢复 `latest` 第三方下载旁路。

## 第三方 component 边界

AgentDock **GitHub Release** 不包含 cloudflared binary；R2 只在独立的 `components/cloudflared/<version>/` 下维护受控第三方镜像。审计过的 `internal/component/catalog-v1.json` 固定：

- cloudflared 明确版本，不跟踪 upstream `latest`；
- Cloudflare 官方 Release URL；
- `download.nexusdock.co` 上同版本同文件名的不可变 mirror URL；
- Windows/Linux `binary` / macOS `tgz` 格式；
- upstream artifact SHA-256；
- AgentDock 兼容范围和状态。

独立 component publish workflow 在对应平台从 Cloudflare 官方来源下载，验证 digest、Authenticode/codesign、归档结构和真实版本后，才允许把原始 artifact 写入 R2。R2 对象以组件版本为不可变 key，已存在对象必须与 pinned SHA-256 一致；发布后再从公网 R2 GET 并重复校验 SHA-256。镜像同时提供 Apache-2.0 LICENSE，且 workflow 会将 vendored LICENSE 与当前 upstream tag 的 LICENSE 做字节级比较，许可证变化必须经过人工审查。AgentDock Release workflow 不写入第三方镜像，只验证 catalog 选中的 `mirror_url` 已存在且 digest 正确。

客户端优先 `mirror_url`；只有镜像请求本身失败时才尝试官方 `url`。若任一成功响应的内容与 pinned SHA-256 不一致，安装直接失败。Windows 的微软共享 Runtime 仍从微软官方固定来源获取并验证，不在 R2 维护副本。

## 发布顺序

Stable 发布：

1. 验证 tag 是合法完整 SemVer，并确认它指向 `main` 中的不可变发布提交；该 tag 是本次发布版本的唯一来源。
2. 构建、签名并验证 Linux、macOS、Windows、容器及第三方 upstream。
3. 要求 catalog 选中的 cloudflared 已经通过独立 component publish workflow 发布到不可变 `components/cloudflared/<version>/`；Release workflow 只验证公网 mirror digest，不负责写入。
4. 创建 GitHub draft，上传不可变 `releases/<tag>/` 到 R2，并验证全部公网版本化 URL 与 component mirror digest。
5. 在 Stable 用户入口仍指向旧版时执行 fail-safe retention。
6. 发布并验证 `components/v1/catalog.json`。
7. 最后更新并验证 `latest.json`。
8. 提升容器 `latest` aliases，并把 GitHub draft 发布为正式 Release。

Prerelease 执行相同的构建、签名、候选验证和不可变 R2 上传，但跳过第 5-7 步和容器 mutable alias promotion；GitHub Release 标记为 prerelease 且不成为 GitHub Latest。

这样任何上传、验证、component repository 或 retention 失败都不会提前移动 Stable 用户入口。

## GitHub Actions 配置

Repository Variables：

- `R2_ACCOUNT_ID`：Cloudflare Account ID；
- `R2_BUCKET`：AgentDock 正式 Release bucket；
- `R2_PUBLIC_BASE_URL`：必须为 `https://download.nexusdock.co`。

Repository Secrets：

- `R2_ACCESS_KEY_ID`
- `R2_SECRET_ACCESS_KEY`

R2 凭据只需要目标 bucket 的对象读写权限，不需要 Cloudflare 账号级管理员权限。

### R2 retention 手动收敛

若历史异常或兼容桥造成 R2 `releases/` 暂时偏离“当前 Stable + 上一 Stable”，使用 `R2 Release Retention Maintenance` workflow 做恢复。该 workflow 默认只生成 retention plan；只有显式启用 `apply` 且 `expected_current_tag` 与 R2 当前 `latest.json` 完全一致时才执行删除，并与正式 Release 共用 `release-publication` 并发锁。生产 R2 凭据继续只保存在 GitHub Secrets，不导出到开发机。
