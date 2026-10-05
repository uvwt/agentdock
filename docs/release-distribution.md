# Release 下载分发

AgentDock 的正式版本仍以 GitHub Release 为唯一历史档案。Release workflow 在正式 GitHub Release 发布成功后，把当前最新版的 **AgentDock 第一方发布物** 镜像到 Cloudflare R2，供 NexusDock 官网和客户端更新使用。

第三方依赖不进入这条镜像链。当前 cloudflared 由 AgentDock 的第一方 component catalog 固定版本、固定官方 URL、artifact format 和 SHA-256；客户端直接从 Cloudflare 官方 GitHub Release 下载。

## R2 对象布局

R2 只保留当前稳定版的 AgentDock 第一方发布物和第一方 metadata：

```text
latest.json
releases/
└── vX.Y.Z/
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

以下内容明确不进入 AgentDock GitHub Release 或 R2：

- `cloudflared_windows_amd64.exe`
- `cloudflared_darwin_amd64`
- `cloudflared_darwin_arm64`
- Microsoft .NET / Windows App Runtime installer

`latest.json` 使用 GitHub Release API 的最小兼容结构：`tag_name` 和 `assets[].name/browser_download_url`。这样客户端以后可以把最新版元数据源切到 NexusDock 下载域名，而不需要维护第二套 Release 数据模型。

发布顺序是：

1. GitHub Release 候选产物经过跨平台验证。
2. Release CI 额外验证仓库 pinned 的第三方依赖：固定 URL、固定 SHA-256、平台信任和版本；只验证，不上传第三方 binary。
3. 正式公开 AgentDock GitHub Release。
4. 下载已验证的 `agentdock-release-dist`。
5. 上传 `releases/<tag>/` 下的 AgentDock 第一方不可变对象并通过 R2 API 校验。
6. 从公开下载域名逐个验证版本化 URL。
7. 最后更新根目录 `latest.json`；成功后再清理 R2 旧版本对象。

任何步骤在更新 `latest.json` 前失败时，旧版下载入口保持不变；更新之后的清理失败只会留下旧对象，不会破坏当前最新版。

## R2 rewrite 边界

R2 中的 `install.sh` 会把默认 AgentDock Release 基础地址改写为同版本的
`https://download.nexusdock.co/releases/<tag>`，并重新生成 `install.sh.sha256`。
GitHub Release 中的原始 `install.sh` 不变，因此历史版本仍保持 GitHub 自身的可复现下载链路。

`agentdock-component-catalog.json` 是 AgentDock 第一方 metadata，因此 catalog 文件本身可以进入 R2；但 catalog 内的第三方 artifact URL **不能**被镜像阶段改写。以 cloudflared 为例，URL 必须始终保持：

```text
https://github.com/cloudflare/cloudflared/releases/download/<pinned-version>/...
```

R2 prepare 只会重新生成 catalog 文件自身的 SHA-256，不会要求本地存在 cloudflared binary，也不会生成 Cloudflare artifact 的 R2 副本。

cloudflared 的信任根位于仓库审计过的 `packaging/components/cloudflared.json`：

- version 固定，不跟踪 `latest`；
- URL 固定为 Cloudflare 官方 Release；
- Windows format 为 `binary`；
- macOS format 为 `tgz`；
- catalog SHA-256 描述下载 artifact；
- 安装后的 `active.json` SHA-256 描述最终本地 binary，两者语义不混用。

## GitHub Actions 配置

Repository Variables：

- `R2_ACCOUNT_ID`：Cloudflare Account ID。
- `R2_BUCKET`：用于最新版 AgentDock 发布物镜像的 R2 bucket。
- `R2_PUBLIC_BASE_URL`：R2 自定义公开域名，例如 `https://download.nexusdock.co`，不要带末尾 `/`。

Repository Secrets：

- `R2_ACCESS_KEY_ID`
- `R2_SECRET_ACCESS_KEY`

R2 凭据只需要目标 bucket 的对象读写权限，不需要 Cloudflare 账号级管理员权限。

历史版本、Release Notes 和旧安装包继续从 GitHub Releases 获取。R2 不承担历史归档，也不承担 Microsoft Runtime 或第三方组件镜像职责。
