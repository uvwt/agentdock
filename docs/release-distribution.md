# Release 下载分发

AgentDock 的官方第一方二进制分发边界统一为 `https://download.nexusdock.co`。桌面端检查更新、安装脚本、AgentDock 归档、Setup/DMG、校验文件和第一方 component catalog 不依赖 AgentDock GitHub Release 下载地址；GitHub Release 继续承担 Release Notes、社区入口和历史下载归档职责。

第三方依赖不进入 AgentDock 镜像链。cloudflared 由 AgentDock 的第一方 component catalog 固定 Cloudflare 官方 Release URL、artifact format 和 SHA-256，客户端直接从 Cloudflare 官方 GitHub Release 下载。Microsoft .NET Windows Desktop Runtime 与 Windows App Runtime 同样由安装器按固定的微软官方来源获取，不上传到 AgentDock R2。

## 产品契约

AgentDock 的公开安装与更新只支持**当前稳定版**：

- `install.sh` 不提供 `--version`；
- `install.ps1` 不提供 `-Version`；
- 桌面 self-update 只读取 `https://download.nexusdock.co/latest.json`；
- component catalog 只通过 `https://download.nexusdock.co/latest/agentdock-component-catalog.json` 获取；
- 客户端运行时不包含 AgentDock GitHub Release fallback；
- GitHub 的历史 Release 可以用于开发、排障和人工归档下载，但不属于产品安装 API。

测试和离线安装仍可通过现有测试/离线入口注入本地 payload 或测试下载基址；这些能力不构成面向用户的历史版本安装功能。

## R2 对象模型

R2 底层仍使用版本化不可变目录，避免覆盖 `latest` 文件造成半发布：

```text
latest.json
releases/
├── vX.Y.Z/       # 上一个稳定版，临时保留用于紧急回退
│   └── ...
└── vX.Y.(Z+1)/   # 当前稳定版
    ├── AgentDockSetup-amd64.exe
    ├── AgentDockSetup-amd64.exe.sha256
    ├── AgentDockSetup-arm64.exe
    ├── AgentDockSetup-arm64.exe.sha256
    ├── AgentDock-macos-universal.dmg
    ├── AgentDock-macos-universal.dmg.sha256
    ├── AgentDock-macos-universal.zip
    ├── AgentDock-macos-universal.zip.sha256
    ├── agentdock-component-catalog.json
    ├── agentdock-component-catalog.json.sha256
    ├── agentdock_{linux,darwin,windows}_{amd64,arm64}.*
    ├── install.sh
    ├── install.sh.sha256
    ├── install.ps1
    └── install.ps1.sha256
```

版本目录内对象不可覆盖：如果同一个 key 已存在但 SHA-256 不同，发布立即失败。这样当前版本在上传、校验完成之前不会影响现有用户。

`latest.json` 是桌面稳定版唯一可变指针，使用最小 GitHub Release 兼容结构：`tag_name` 和 `assets[].name/browser_download_url`。所有 `browser_download_url` 指向当前版本的 `https://download.nexusdock.co/releases/<tag>/...`。

下载 Worker 为官网保留平台友好别名，同时允许安全的 `/latest/<asset-name>` 从 `latest.json` 解析到当前版本化对象，例如：

- `/latest/install.sh`
- `/latest/agentdock_linux_amd64.tar.gz`
- `/latest/agentdock-component-catalog.json`

Android 使用独立的 `android/latest.json` / `android/releases/` 分发契约，不与桌面 `latest.json` 混用。

## R2 保留策略

R2 只保留两个完整稳定版本：

1. 当前 `latest.json` 指向的稳定版；
2. 发布前 `latest.json` 指向的上一稳定版。

新版本完成上传并通过公开 URL 校验后，Release workflow 在切换 `latest.json` **之前**删除更早的 `releases/<tag>/` 前缀，只保留候选新版本和发布前 `latest.json` 指向的当前稳定版。清理失败时 workflow 直接停止，用户入口仍停留在旧稳定版。

如果同一 Release workflow 被重跑，或者发布前记录的上一稳定版前缀已经不存在，workflow 不猜测其他前缀的语义，也不执行删除；宁可暂时多保留对象，也不把失败上传的半成品误当成上一稳定版。下一次正常新版本发布会再次按当前/上一稳定版策略收敛。

正常发布完成后，R2 目标状态是两个完整稳定版本。它们是发布原子性和紧急回退缓冲，不是公开的历史版本安装功能。

现有较老 GitHub Release **不迁移回 R2**。GitHub 继续保存历史 Release；R2 从本策略生效后只维护当前与上一稳定版。

## Bootstrap 约定

源码仓库和正式 Release 中的 `install.sh` / `install.ps1` 都只访问：

```text
https://download.nexusdock.co/latest
```

Release 打包阶段不再二次改写 bootstrap 为版本化下载地址，也不再维护 `prepare-distribution` 之类的脚本重写步骤。GitHub Release 与 R2 直接发布同一份 bootstrap 字节及其 checksum。

bootstrap 下载到 payload 后，实际安装版本由 payload 自身的版本元数据确定；Windows 安装结果也记录实际 payload 版本，而不是信任外部传入的版本参数。

## 第三方 component 边界

`agentdock-component-catalog.json` 是 AgentDock 第一方 metadata，因此 catalog 文件本身进入 GitHub Release 和 R2；catalog 中 cloudflared 的 artifact URL 始终保持 Cloudflare 官方固定版本地址，不能改写为 NexusDock R2。

cloudflared 的信任根位于仓库审计过的 `packaging/components/cloudflared.json`：

- version 固定，不跟踪 `latest`；
- URL 固定为 Cloudflare 官方 Release；
- Windows format 为 `binary`；
- macOS format 为 `tgz`；
- upstream artifact SHA-256 固定；
- Release CI 在对应平台验证 digest、签名/代码签名、归档结构和版本；
- AgentDock Release/R2 不包含 cloudflared binary。

Windows 的微软共享 Runtime 采用同一原则：AgentDock Setup 只携带固定 metadata/安装逻辑，缺失时从微软官方固定来源获取并验证 Authenticode，不在 R2 维护第三方副本。

## 发布顺序

1. 构建、签名并生成唯一 AgentDock release candidate，同时生成固定官方第三方来源的 component catalog。
2. 完成 Linux、macOS、Windows、容器、第三方 upstream 和 GitHub draft 验证。
3. 上传 `releases/<tag>/` 的第一方资产到 R2；同 key 不允许不同 digest 覆盖。
4. 从 `download.nexusdock.co/releases/<tag>/...` 逐个验证公开版本化 URL。
5. 读取发布前的 `latest.json` tag，并在用户入口仍指向旧稳定版时，保留候选新 tag 与当前稳定 tag、删除更早 R2 Release 前缀。
6. 清理成功后更新并验证根目录 `latest.json`，此时候选新 tag 成为当前稳定版，旧稳定 tag 成为上一稳定版。
7. 最后公开 GitHub Release，并提升容器 `latest` aliases。

这样 R2 上传、公开 URL 校验或保留策略执行失败时都不会提前切换用户入口；只有这些步骤全部成功后才移动 `latest.json`，同时保持一个上一稳定版用于紧急回退。

## GitHub Actions 配置

Repository Variables：

- `R2_ACCOUNT_ID`：Cloudflare Account ID。
- `R2_BUCKET`：AgentDock 正式 Release bucket。
- `R2_PUBLIC_BASE_URL`：公开分发域名，当前必须为 `https://download.nexusdock.co`。

Repository Secrets：

- `R2_ACCESS_KEY_ID`
- `R2_SECRET_ACCESS_KEY`

R2 凭据只需要目标 bucket 的对象读写权限，不需要 Cloudflare 账号级管理员权限。
