# Release 下载分发

AgentDock 的正式版本仍以 GitHub Release 为唯一历史档案。`Release` workflow 在正式 GitHub Release 发布成功后，把当前最新版镜像到 Cloudflare R2，供 NexusDock 官网和后续客户端更新使用。

## R2 对象布局

R2 只保留当前稳定版：

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
    ├── cloudflared_darwin_amd64
    ├── cloudflared_darwin_amd64.sha256
    ├── cloudflared_darwin_arm64
    ├── cloudflared_darwin_arm64.sha256
    ├── cloudflared_windows_amd64.exe
    ├── cloudflared_windows_amd64.exe.sha256
    ├── agentdock_{linux,darwin,windows}_{amd64,arm64}.*
    ├── install.sh
    ├── install.sh.sha256
    ├── install.ps1
    └── install.ps1.sha256
```

`latest.json` 使用 GitHub Release API 的最小兼容结构：`tag_name` 和 `assets[].name/browser_download_url`。这样客户端以后可以把最新版元数据源切到 NexusDock 下载域名，而不需要维护第二套 Release 数据模型。

发布顺序是：

1. GitHub Release 经过现有跨平台验证并正式公开。
2. 下载已验证的 `agentdock-release-dist`。
3. 上传 `releases/<tag>/` 下的不可变对象并通过 R2 API 校验。
4. 从公开下载域名逐个验证版本化 URL。
5. 最后更新根目录 `latest.json`，避免元数据提前指向未完成上传的版本。
6. `latest.json` 验证成功后，删除 `releases/` 下其他版本对象。

R2 中的 `install.sh` 会在镜像阶段把默认 Release 基础地址改写为同版本的
`https://download.nexusdock.co/releases/<tag>`，并重新生成 `install.sh.sha256`。
GitHub Release 中的原始 `install.sh` 不变，因此历史版本仍保持 GitHub 自身的可复现下载链路。

`agentdock-component-catalog.json` 也会在 R2 镜像阶段重写为同版本的
`https://download.nexusdock.co/releases/<tag>/...` 组件 URL，并重新生成自己的 SHA-256。
正式 GitHub Release 中的 catalog 则保持指向该 GitHub Release 的不可变资产。这样
AgentDock 与 cloudflared 可以独立更新，但两种分发入口都使用同一份固定版本、固定 digest
和 upstream provenance 契约；基础 Setup/DMG 本身不携带 cloudflared。

任何步骤在更新 `latest.json` 前失败时，旧版下载入口保持不变；更新之后的清理失败只会留下旧对象，不会破坏当前最新版。

## GitHub Actions 配置

Repository Variables：

- `R2_ACCOUNT_ID`：Cloudflare Account ID。
- `R2_BUCKET`：用于最新版安装包镜像的 R2 bucket。
- `R2_PUBLIC_BASE_URL`：R2 自定义公开域名，例如 `https://download.nexusdock.co`，不要带末尾 `/`。

Repository Secrets：

- `R2_ACCESS_KEY_ID`
- `R2_SECRET_ACCESS_KEY`

R2 凭据只需要目标 bucket 的对象读写权限，不需要 Cloudflare 账号级管理员权限。

历史版本、Release Notes 和旧安装包继续从 GitHub Releases 获取。R2 不承担历史归档职责。
