# Release 下载分发

AgentDock 的官方第一方二进制分发边界统一为 `https://download.nexusdock.co`。桌面端检查更新、安装脚本、AgentDock 归档、Setup/DMG、校验文件和第一方 component catalog 不依赖 AgentDock GitHub Release 下载地址；GitHub Release 继续承担 Release Notes、社区入口和下载镜像职责。

第三方依赖不进入 AgentDock 镜像链。cloudflared 由 AgentDock 的第一方 component catalog 固定 Cloudflare 官方 Release URL、artifact format 和 SHA-256，客户端直接从 Cloudflare 官方 GitHub Release 下载。Microsoft .NET Windows Desktop Runtime 与 Windows App Runtime 同样由安装器按固定的微软官方来源获取，不上传到 AgentDock R2。

正式版本遵循以下原则：

1. `releases/vX.Y.Z/` 是 AgentDock 第一方不可变版本目录，发布后不覆盖、不自动删除。
2. `latest.json` 是唯一可变的桌面稳定版指针，只在新版本第一方资产全部上传并验证成功后更新。
3. GitHub Release 与 R2 发布同一份 AgentDock 第一方 release candidate；不再为 R2 二次改写制品。
4. 第三方 binary 只做固定版本、固定 digest 和平台信任验证，不因 AgentDock Release 再托管一份。

## R2 对象布局

```text
latest.json
releases/
├── v0.9.1/
│   └── ...
├── vX.Y.Z/
│   ├── AgentDockSetup-amd64.exe
│   ├── AgentDockSetup-amd64.exe.sha256
│   ├── AgentDockSetup-arm64.exe
│   ├── AgentDockSetup-arm64.exe.sha256
│   ├── AgentDock-macos-universal.dmg
│   ├── AgentDock-macos-universal.dmg.sha256
│   ├── AgentDock-macos-universal.zip
│   ├── AgentDock-macos-universal.zip.sha256
│   ├── agentdock-component-catalog.json
│   ├── agentdock-component-catalog.json.sha256
│   ├── agentdock_{linux,darwin,windows}_{amd64,arm64}.*
│   ├── install.sh
│   ├── install.sh.sha256
│   ├── install.ps1
│   └── install.ps1.sha256
```

以下内容明确不进入 AgentDock GitHub Release 或 R2：

- cloudflared binary / archive；
- Microsoft .NET Windows Desktop Runtime installer；
- Microsoft Windows App Runtime installer。

`latest.json` 使用最小 GitHub Release 兼容结构：`tag_name` 和 `assets[].name/browser_download_url`。所有 `browser_download_url` 都指向同版本的 `https://download.nexusdock.co/releases/<tag>/...`。

下载 Worker 为官网保留平台友好别名，同时允许安全的 `/latest/<asset-name>` 从 `latest.json` 解析到版本化对象，例如：

- `/latest/install.sh`
- `/latest/agentdock_linux_amd64.tar.gz`
- `/latest/agentdock-component-catalog.json`

Android 使用独立的 `android/latest.json` / `android/releases/` 分发契约，不与桌面 `latest.json` 混用。

## Bootstrap 约定

源码仓库中的 `install.sh` 和 `install.ps1` 默认从 `https://download.nexusdock.co/latest` 获取当前桌面稳定版；显式指定 `vX.Y.Z` 时使用 `https://download.nexusdock.co/releases/vX.Y.Z`。

正式 release candidate 在生成阶段就把两个脚本的默认地址固定为自己的版本目录，并重新生成脚本 SHA-256。因此未来从 GitHub Release 或 R2 取得同一个 `vX.Y.Z` 脚本时，文件字节一致，且默认安装该版本，不会漂移到未来最新版。

## 第三方 component 边界

`agentdock-component-catalog.json` 是 AgentDock 第一方 metadata，因此 catalog 文件本身进入 GitHub Release 和 R2；catalog 中 cloudflared 的 artifact URL 则始终保持 Cloudflare 官方固定版本地址，不能被改写为 NexusDock R2。

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
3. 上传 `releases/<tag>/` 的第一方资产到 R2。对象写入 `sha256` metadata；若同 key 已存在但 digest 不同，发布立即失败，防止覆盖不可变版本。
4. 从 `download.nexusdock.co/releases/<tag>/...` 逐个验证公开版本化 URL。
5. 更新并验证根目录 `latest.json`。
6. 最后公开 GitHub Release，并提升容器 `latest` aliases。

这样 R2 上传或公开验证失败时，旧 `latest.json` 和公开 GitHub Release 都不会提前切换；已发布的 R2 版本目录长期保留，不再执行“新版本发布后删除旧版本”的清理步骤。

## 历史归档修复

旧版发布流程曾只保留 R2 当前稳定版，因此较早 GitHub Release 可能没有对应 R2 版本目录。历史修复必须遵守不可变原则：只把当时 GitHub Release 中真实存在的资产原样补到 `releases/<tag>/`，不改写旧脚本、不补造不存在的 catalog、不修改 `latest.json`。

修复流程应按版本检查目标 prefix；只有整个版本 prefix 缺失时才自动补齐。若目标 prefix 已存在部分对象，则停止并人工核对，不能用“覆盖上传”猜测状态。

## GitHub Actions 配置

Repository Variables：

- `R2_ACCOUNT_ID`：Cloudflare Account ID。
- `R2_BUCKET`：AgentDock 正式 Release bucket。
- `R2_PUBLIC_BASE_URL`：公开分发域名，当前必须为 `https://download.nexusdock.co`。

Repository Secrets：

- `R2_ACCESS_KEY_ID`
- `R2_SECRET_ACCESS_KEY`

R2 凭据只需要目标 bucket 的对象读写权限，不需要 Cloudflare 账号级管理员权限。
