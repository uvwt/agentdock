# AgentDock

AgentDock 是一个用 Go 编写的 Model Context Protocol（MCP）工具服务。它把一个本地或容器内的工作空间暴露给支持 MCP 的客户端，让模型可以安全地读取文件、搜索代码、编辑文件、执行受控命令、管理长任务会话、处理 Git 仓库，并查看图片资源。

这个项目适合用作 ChatGPT / MCP 客户端的代码工作区后端，也适合放进 Docker 容器里作为轻量级代码代理工具服务。

## 核心特性

- **MCP 兼容**：提供 `initialize`、`tools/list`、`tools/call` 等 MCP JSON-RPC 接口。
- **HTTP 与 stdio 双传输**：既可以作为 HTTP MCP Server 运行，也可以通过 stdio 嵌入其他宿主。
- **单 workspace 边界**：所有文件路径都限制在配置的 workspace 内，避免越界访问宿主文件系统。
- **多项目工作区**：`/workspace` 下可以同时放多个项目，通过 `repo_path` 指定具体 Git 仓库。
- **文件与代码工具**：支持读取文件、列目录、按 glob 查找文件、搜索文本、应用补丁。
- **命令会话工具**：支持受控执行命令、长任务会话、增量读取 stdout/stderr、停止单个或全部会话。
- **Git 专用工具**：支持仓库发现、状态、diff、log、show、blame、fetch、pull、push、clone、commit。
- **GitHub token 辅助**：支持从 `.env` 读取 GitHub token 并配置 HTTPS credential，输出始终脱敏。
- **动态 Connector**：支持从 AgentDock 加载 `connectors` 动态能力，新增应用脚本不需要重新编译 MCP。
- **可选浏览器自动化**：可启用浏览器自动化工具，通过 AgentDock 内 Node runner 调用 Playwright；默认不开启、不安装 Chromium。
- **结构化输出**：工具返回 `structuredContent`，并提供 `outputSchema`，方便 MCP 客户端理解结果。
- **日志与排障**：默认输出 JSON 日志到 stderr，可通过 `docker logs` 或 `docker compose logs` 查看。
- **Linux 沙箱增强**：在 Linux 下 best-effort 使用 Landlock 限制命令文件系统访问；不支持时会返回 warning。

## 适用场景

- 给 ChatGPT 或其他 MCP 客户端提供代码编辑、搜索、构建、测试能力。
- 在 Docker 中启动一个隔离 workspace，让模型只操作容器内代码。
- 管理一个包含多个项目的工作空间，例如：

```text
/workspace/
  service-a/
    .git/
  web-ui/
    .git/
  scripts/
    .git/
```

- 需要模型执行 Git 提交、推送、拉取、代码搜索、补丁应用等自动化任务。

## 工具概览

### 服务与工作区

| 工具 | 说明 |
| --- | --- |
| `server_info` | 返回服务版本、workspace、工具列表、沙箱状态等信息。 |
| `tool_descriptors` | 返回当前服务实际暴露的工具 descriptor，用于排查客户端缓存问题。 |
| `get_default_cwd` | 查看默认工作目录。 |
| `set_default_cwd` | 设置默认工作目录。 |
| `workspace_repos` | 扫描 workspace 下的 Git 仓库，并返回分支、remote、clean、ahead/behind 状态。 |

### 文件与搜索

| 工具 | 说明 |
| --- | --- |
| `read_file` | 读取 UTF-8 文本文件，可指定行号和最大字节数。 |
| `list_dir` | 列目录，支持递归、隐藏文件、忽略规则。 |
| `list_files` | 按 glob 查找文件，支持 `**/*.go`、`internal/**/*.go` 等模式。 |
| `search_text` | 搜索文本或正则，支持 include/exclude glob 与上下文行。 |
| `apply_patch` | 应用结构化补丁，支持 dry-run。 |
| `view_image` | 返回图片内容，支持大小限制和自动缩放。 |

### 命令与会话

| 工具 | 说明 |
| --- | --- |
| `exec_command` | 在 workspace 内执行受控命令，支持超时、长任务会话和输出截断。 |
| `write_stdin` | 向运行中的命令会话写入 stdin。 |
| `session_status` | 获取运行中会话的增量 stdout/stderr 和状态。 |
| `list_sessions` | 列出当前运行中的命令会话。 |
| `kill_session` | 停止指定命令会话。 |
| `kill_all_sessions` | 停止所有运行中的命令会话。 |

### Git 与 GitHub

| 工具 | 说明 |
| --- | --- |
| `git_repo_status` / `git_status` | 查看指定仓库状态。多项目场景建议传 `repo_path`。 |
| `git_diff` | 查看指定仓库 diff。 |
| `git_log` | 查看提交历史。 |
| `git_show` | 查看指定 revision。 |
| `git_blame` | 查看文件 blame。 |
| `git_fetch` | 拉取远程 refs。 |
| `git_pull` | 拉取并合并远程分支。 |
| `git_push` | 推送指定分支到远程。 |
| `git_clone` | 克隆仓库到 workspace。 |
| `git_commit` | 暂存并创建提交。 |
| `configure_github_token` | 从 `.env` 读取 GitHub token 并配置 Git HTTPS credential。 |
| `check_github_repo_access` | 检查 GitHub token 是否能认证并访问指定仓库。 |

### 动态 Connector

| 工具 | 说明 |
| --- | --- |
| `connector_list` | 列出 workspace 中已安装的动态 connector。 |
| `connector_describe` | 查看指定 connector 的说明、动作和输入 schema。 |
| `connector_call` | 调用指定 connector action，并传入结构化参数。 |

### 可选浏览器自动化

浏览器工具默认不暴露。Docker 浏览器增强镜像会自动准备 runner 和 Chromium；启用 `AGENTDOCK_BROWSER_ENABLED=true` 后，工具列表会出现以下工具。

| 工具 | 说明 |
| --- | --- |
| `browser_session_start` | 创建浏览器自动化会话。 |
| `browser_action` | 执行 goto、click、fill、wait、scroll 等动作，并返回页面快照。 |
| `browser_snapshot` | 获取当前页面 URL、标题、文本摘要、截图路径、截图 artifact id、console/network 错误。 |
| `browser_session_close` | 关闭浏览器会话。 |

浏览器截图产物默认写入 AgentDock 控制目录：

```text
/agent-dock/browser-artifacts/screenshots/
```

`browser_action` 和 `browser_snapshot` 默认返回：

```json
{
  "screenshot_path": "/agent-dock/browser-artifacts/screenshots/snapshot-xxx.png",
  "screenshot_artifact_id": "browser-screenshot-xxxxxxxxxxxxxxxx"
}
```

如果配置了 `AGENTDOCK_SERVER_URL`，还会额外返回可访问的截图地址：

```json
{
  "screenshot_url": "https://codingmini.example.com/artifacts/browser/screenshots/snapshot-xxx.png"
}
```

AgentDock 内置了截图静态访问路由：

```text
GET  /artifacts/browser/screenshots/<filename>.png
HEAD /artifacts/browser/screenshots/<filename>.png
```

该路由只允许访问浏览器截图目录下的 `.png` 文件，不允许 `../` 路径穿越。公网部署时建议放在 HTTPS 反向代理之后，并按需要在代理层增加访问控制。

如果调用方确实需要直接拿到图片内容，可以在 `browser_action` 或 `browser_snapshot` 参数中传：

```json
{
  "include_screenshot_base64": true
}
```

此时响应会额外包含：

```json
{
  "screenshot_mime_type": "image/png",
  "screenshot_base64": "..."
}
```

`include_screenshot_base64` 默认关闭，因为截图可能很大，会增加响应体积和延迟。

## 多项目 workspace 模型

本服务把 `workspace` 当作总工作区，而不是默认把 workspace 本身当成 Git 仓库。

推荐结构：

```text
/workspace/
  agentdock/
    .git/
  another-project/
    .git/
```

Git 工具建议显式传入：

```json
{
  "repo_path": "agentdock"
}
```

这样可以避免多项目场景下误操作到错误仓库。

示例：

```json
{
  "repo_path": "agentdock",
  "remote": "origin",
  "branch": "main"
}
```

## 安装与构建

要求：

- Go 1.22+
- Git
- Docker 可选

本地测试和构建：

```bash
go test ./...
go vet ./...
go build ./cmd/agentdock
```

运行二进制：

```bash
./agentdock --workspace /path/to/workspace --host 127.0.0.1 --port 8765
```

HTTP MCP endpoint：

```text
http://127.0.0.1:8765/mcp
```

stdio 模式：

```bash
./agentdock --stdio --workspace /path/to/workspace
```

## Docker 使用

推荐把“源码构建目录”和“运行数据目录”分开：

```text
/opt/agentdock/   # 源码目录，只负责 git pull 和 docker build
/srv/agentdock/   # 运行目录，放 compose、workspace、AgentDock 数据
```

这样 `workspace` 和 `AgentDock` 不会混进源码仓库。

### 第一步：在源码目录构建镜像

基础镜像：

```bash
cd /opt/agentdock
docker build -t agentdock:local .
```

浏览器增强镜像：

```bash
cd /opt/agentdock
docker build -f Dockerfile.browser -t agentdock:browser .
```

`Dockerfile.browser` 是自包含的，会自己从源码构建 AgentDock，并安装 Playwright runner 和 Chromium，不依赖 `agentdock:local`。

### 第二步：在运行目录启动容器

创建运行目录：

```bash
sudo mkdir -p /srv/agentdock
sudo chown -R $USER:$USER /srv/agentdock
cd /srv/agentdock
```

基础模式 `docker-compose.yml`：

```yaml
services:
  agentdock:
    image: agentdock:local
    container_name: agentdock
    restart: unless-stopped
    ports:
      - "127.0.0.1:18765:8765"
    volumes:
      - ./workspace:/workspace
      - ./AgentDock:/agent-dock
    environment:
      AGENTDOCK_TOOL_PROFILE: "full"
      AGENTDOCK_DIR: "/agent-dock"
      AGENTDOCK_SKIP_PERMISSION_PROMPTS: "true"
    command:
      - agentdock
      - --workspace
      - /workspace
      - --host
      - 0.0.0.0
      - --port
      - "8765"
      - --dangerously-skip-all-permissions
```

如果需要 OAuth，把环境变量补进去：

```yaml
      AGENTDOCK_OAUTH_CLIENT_ID: "coding-tools-client"
      AGENTDOCK_OAUTH_CLIENT_SECRET: "replace-with-secret"
      AGENTDOCK_OAUTH_PASSWORD: "replace-with-password"
      AGENTDOCK_OAUTH_TOKEN_SECRET: "replace-with-token-secret"
      AGENTDOCK_SERVER_URL: "https://codingmini.example.com"
```

浏览器增强模式只需要把镜像改成 `agentdock:browser`，并启用浏览器工具：

```yaml
    image: agentdock:browser
    environment:
      AGENTDOCK_BROWSER_ENABLED: "true"
```

启动：

```bash
docker compose up -d
```

查看状态和日志：

```bash
docker compose ps
docker compose logs -f
```

运行目录结构会是：

```text
/srv/agentdock/
  docker-compose.yml
  workspace/              # 用户项目工作区
  AgentDock/              # AgentDock 控制层
    connectors/           # 动态 connector
    browser-runner/       # 浏览器 runner，浏览器镜像会自动初始化
    browser-artifacts/    # 截图、状态、trace 等产物
      screenshots/        # browser_snapshot / browser_action 生成的 PNG 截图
```

浏览器截图由 `browser_snapshot` 和 `browser_action` 自动生成。默认返回：

```json
{
  "screenshot_path": "/agent-dock/browser-artifacts/screenshots/snapshot-xxx.png",
  "screenshot_artifact_id": "browser-screenshot-xxxxxxxxxxxxxxxx"
}
```

如果配置了 `AGENTDOCK_SERVER_URL`，还会额外返回 `screenshot_url`：

```json
{
  "screenshot_url": "https://codingmini.example.com/artifacts/browser/screenshots/snapshot-xxx.png"
}
```

对应的 HTTP 静态访问路由是：

```text
GET /artifacts/browser/screenshots/<filename>.png
HEAD /artifacts/browser/screenshots/<filename>.png
```

该路由只允许访问 `AgentDock/browser-artifacts/screenshots/` 下的 PNG 文件，并拒绝 `../` 等路径穿越。

没有配置 `AGENTDOCK_SERVER_URL` 时，截图仍会保存并返回 `screenshot_path` / `screenshot_artifact_id`，但不会返回 `screenshot_url`。

如需把图片内容直接放进工具响应，可以在调用 `browser_snapshot` 或 `browser_action` 时传：

```json
{
  "include_screenshot_base64": true
}
```

此时响应会额外包含：

```json
{
  "screenshot_mime_type": "image/png",
  "screenshot_base64": "..."
}
```

`include_screenshot_base64` 默认关闭，因为截图可能较大，会显著增加工具响应体积。

新增或修改 connector 时，只要编辑：

```text
/srv/agentdock/AgentDock/connectors/
```

不需要重新构建镜像，也不需要重启容器。只有修改 Go 核心代码、安装新的系统依赖，或更换镜像时才需要重新构建/重启。

### 更新流程

源码目录更新并重新构建镜像：

```bash
cd /opt/agentdock
git pull
go test ./...
docker build -t agentdock:local .
# 如果使用浏览器增强镜像：
docker build -f Dockerfile.browser -t agentdock:browser .
```

运行目录重启容器：

```bash
cd /srv/agentdock
docker compose up -d
docker compose logs -f
```


## macOS 裸机部署

AgentDock 可以在 macOS 上裸机运行，适合本地项目管理、Git 自动化、文件处理、动态 Connector 和浏览器自动化测试。macOS 没有 Linux Landlock，也没有 systemd，所以它更适合本地开发/自动化，不建议当作生产部署服务器。

推荐目录：

```text
~/agentdock-workspace/   # 用户项目工作区
~/AgentDock/             # AgentDock 控制层
```

构建：

```bash
git clone https://github.com/uvwt/agentdock.git
cd agentdock

go test ./...
go build -trimpath -o agentdock ./cmd/agentdock
```

运行：

```bash
mkdir -p ~/agentdock-workspace ~/AgentDock

AGENTDOCK_BROWSER_ENABLED=false ./agentdock \
  --workspace ~/agentdock-workspace \
  --agentdock-dir ~/AgentDock \
  --host 127.0.0.1 \
  --port 8765 \
  --oauth-mode \
  --tool-profile full \
  --sandbox-mode none
```

macOS 下 `sandbox-mode=landlock` 不会启用 Linux Landlock，`server_info.sandbox.enabled` 会显示为 `false`。这是平台限制，不是配置错误。

### macOS 浏览器自动化

macOS 裸机需要自己安装 Node 和 Playwright：

```bash
brew install node
mkdir -p ~/AgentDock/browser-runner ~/AgentDock/browser-artifacts
cp -R examples/browser-runner/. ~/AgentDock/browser-runner/

cd ~/AgentDock/browser-runner
npm install
npx playwright install chromium
```

然后启用浏览器工具：

```bash
AGENTDOCK_BROWSER_ENABLED=true ./agentdock \
  --workspace ~/agentdock-workspace \
  --agentdock-dir ~/AgentDock \
  --host 127.0.0.1 \
  --port 8765 \
  --oauth-mode \
  --tool-profile full \
  --sandbox-mode none
```

Docker 浏览器增强镜像会固定 `PLAYWRIGHT_BROWSERS_PATH=/ms-playwright`。macOS 裸机不会强行使用这个路径；如果你没有设置该环境变量，Playwright 会使用自己的默认缓存目录。

## 裸机 VPS 部署

裸机部署会让 MCP 服务直接运行在 VPS 上，`exec_command` 执行的命令也会在 VPS 上运行。和 Docker 部署不同，裸机部署不再有容器隔离，所以建议使用普通 Linux 用户运行服务，并把 workspace 限制在单独目录里。

推荐目录结构：

```text
/opt/agentdock/        # 项目源码
/usr/local/bin/agentdock  # 编译后的可执行文件
/srv/coding-workspace/           # MCP 可操作的项目工作区
/etc/agentdock/env        # 环境变量和密钥
/etc/systemd/system/agentdock.service
```

构建并安装二进制：

```bash
cd /opt
sudo git clone https://github.com/uvwt/agentdock.git
sudo chown -R $USER:$USER /opt/agentdock

cd /opt/agentdock
go test ./...
go build -trimpath -o agentdock ./cmd/agentdock
sudo install -m 0755 agentdock /usr/local/bin/agentdock
```

创建 workspace：

```bash
sudo mkdir -p /srv/coding-workspace
sudo chown -R $USER:$USER /srv/coding-workspace
```

创建环境变量文件：

```bash
sudo mkdir -p /etc/agentdock
sudo nano /etc/agentdock/env
```

示例内容：

```bash
AGENTDOCK_OAUTH_CLIENT_ID=coding-tools-client
AGENTDOCK_OAUTH_CLIENT_SECRET=replace-with-random-secret
AGENTDOCK_OAUTH_PASSWORD=replace-with-login-password
AGENTDOCK_OAUTH_TOKEN_SECRET=replace-with-random-secret
AGENTDOCK_SERVER_URL=https://codingvps.example.com
AGENTDOCK_TOOL_PROFILE=full
AGENTDOCK_LOG_LEVEL=info

# 默认 landlock。裸机 VPS 如需在 exec_command 内使用 sudo，改成 none。
AGENTDOCK_SANDBOX_MODE=none
```

收紧权限：

```bash
sudo chmod 600 /etc/agentdock/env
```

创建 systemd 服务：

```bash
sudo nano /etc/systemd/system/agentdock.service
```

示例内容，其中 `User` / `Group` 替换成你的 Linux 用户名：

```ini
[Unit]
Description=AgentDock
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
EnvironmentFile=/etc/agentdock/env
WorkingDirectory=/srv/coding-workspace
ExecStart=/usr/local/bin/agentdock \
  --workspace /srv/coding-workspace \
  --host 127.0.0.1 \
  --port 8765 \
  --oauth-mode \
  --tool-profile full \
  --sandbox-mode none \
  --dangerously-skip-all-permissions

Restart=always
RestartSec=3
User=your-linux-user
Group=your-linux-user

# 如果 sandbox-mode=none 且需要 sudo，这里不要设成 true。
NoNewPrivileges=false
PrivateTmp=true

[Install]
WantedBy=multi-user.target
```

启动服务：

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now agentdock
sudo systemctl status agentdock
```

查看日志：

```bash
journalctl -u agentdock -f
```

本机健康检查：

```bash
curl http://127.0.0.1:8765/healthz
```

如果需要通过公网域名访问，建议用 Caddy / Nginx 反向代理到 `127.0.0.1:8765`，并只暴露 HTTPS。

### sandbox-mode 说明

`sandbox-mode` 控制 `exec_command` 子进程是否启用内部 Landlock 沙箱。

| 模式 | 说明 |
| --- | --- |
| `landlock` | 默认模式。启用 Linux Landlock，并设置 `no_new_privs`。更安全，但 `sudo` 无法提权。适合 Docker 或不需要 sudo 的场景。 |
| `none` | 不启用内部 Landlock。裸机 VPS 可信部署时可用，允许 `sudo` 按系统 sudoers 规则提权。风险更高，需要配合最小权限用户和 sudoers 白名单。 |

如果你在 MCP 里执行 `sudo` 看到：

```text
sudo: The "no new privileges" flag is set, which prevents sudo from running as root.
```

说明当前命令仍处于 `landlock` 模式，或 systemd 服务还没有重启到新配置。裸机可信部署需要设置：

```bash
AGENTDOCK_SANDBOX_MODE=none
```

或者启动参数：

```bash
--sandbox-mode none
```

然后重新加载并重启服务：

```bash
sudo systemctl daemon-reload
sudo systemctl restart agentdock
```

### 裸机更新流程

以后更新 VPS 上的服务，执行：

```bash
cd /opt/agentdock
git pull

go test ./...
go build -trimpath -o agentdock ./cmd/agentdock

sudo install -m 0755 agentdock /usr/local/bin/agentdock
sudo systemctl restart agentdock
sudo systemctl status agentdock
```

确认新版本启动参数和日志：

```bash
journalctl -u agentdock -n 100 --no-pager
curl http://127.0.0.1:8765/healthz
```

## 配置项

配置可以通过环境变量或命令行参数传入。

| 环境变量 | 默认值 | 说明 |
| --- | --- | --- |
| `AGENTDOCK_WORKSPACE` | `.` | workspace 根目录。 |
| `AGENTDOCK_HOST` | `127.0.0.1` | HTTP 监听地址。 |
| `AGENTDOCK_PORT` | `8765` | HTTP 监听端口。 |
| `AGENTDOCK_AUTH_TOKEN` | 空 | 可选 Bearer token。 |
| `AGENTDOCK_TOOL_PROFILE` | `full` | 工具集配置，支持 `full`、`read-only`、`compat-readonly-all`。 |
| `AGENTDOCK_ENABLE_VIEW_IMAGE` | `true` | 是否暴露 `view_image` 工具。 |
| `AGENTDOCK_STDIO` | `false` | 是否使用 stdio 模式。 |
| `AGENTDOCK_SKIP_PERMISSION_PROMPTS` | `false` | 是否跳过命令策略确认。仅建议在受信容器中使用。 |
| `AGENTDOCK_LOG_LEVEL` | `info` | 日志级别：`debug`、`info`、`warn`、`error`。 |
| `AGENTDOCK_SANDBOX_MODE` | `landlock` | 命令沙箱模式，支持 `landlock`、`none`。裸机需要 sudo 时设为 `none`。 |
| `AGENTDOCK_DIR` | `AgentDock` | AgentDock 控制层目录。connector、browser runner、artifacts 默认相对该目录解析。 |
| `AGENTDOCK_CONNECTOR_DIR` | `connectors` | AgentDock-relative 动态 connector 目录。 |
| `AGENTDOCK_BROWSER_ENABLED` | `false` | 是否暴露可选浏览器自动化工具。 |
| `AGENTDOCK_BROWSER_RUNNER_DIR` | `browser-runner` | AgentDock-relative Node browser runner 目录。 |
| `AGENTDOCK_BROWSER_ARTIFACT_DIR` | `browser-artifacts` | AgentDock-relative 截图、状态和 trace 等浏览器产物目录。 |

常用命令行参数：

```bash
./agentdock \
  --workspace /workspace \
  --host 0.0.0.0 \
  --port 8765 \
  --agentdock-dir /agent-dock \
  --sandbox-mode landlock \
  --log-level info
```

## 动态 Connector

Connector 用于把具体应用能力做成热插拔脚本，避免每新增一个场景都改 Go 代码、重建镜像和刷新 MCP 工具列表。

默认目录：

```text
<AgentDock>/connectors/
```

每个 connector 是一个子目录：

```text
connectors/
  hello/
    connector.json
    scripts/
      echo.sh
```

最小 `connector.json` 示例：

```json
{
  "name": "hello",
  "description": "Example connector that echoes structured input.",
  "version": "0.1.0",
  "actions": {
    "echo": {
      "description": "Echo CONNECTOR_ARGS_JSON as JSON output.",
      "command": "./scripts/echo.sh",
      "timeout_ms": 10000,
      "output": "json",
      "input_schema": {
        "type": "object",
        "additionalProperties": true
      }
    }
  }
}
```

脚本可以从环境变量读取结构化参数：

```sh
#!/usr/bin/env sh
set -eu
printf '{"ok":true,"args":%s}\n' "${CONNECTOR_ARGS_JSON:-{}}"
```

调用流程：

```text
connector_list
connector_describe(connector="hello")
connector_call(connector="hello", action="echo", args={"message":"hi"})
```

Connector 运行时会收到这些环境变量：

| 环境变量 | 说明 |
| --- | --- |
| `CONNECTOR_NAME` | connector 名称。 |
| `CONNECTOR_ACTION` | action 名称。 |
| `CONNECTOR_ARGS_JSON` | 调用方传入的结构化参数 JSON。 |
| `WORKSPACE` | workspace 根目录。 |

注意事项：

- 当前 MVP 使用 `connector.json`，暂不解析 YAML。
- connector 目录在 AgentDock 内，默认是 `<AgentDock>/connectors`。
- `command` 在 connector 目录下执行，建议使用相对路径调用 `scripts/` 下脚本。
- `output=json` 时，MCP 会尝试把 stdout 解析成 JSON 并放入返回的 `json` 字段。
- 如果 connector 需要密钥，建议从服务环境变量读取，并在 `connector.json` 的 `secrets` 中声明变量名，MCP 会在描述时只返回是否配置，不返回密钥内容。
- 新增/修改 connector 文件不需要重启 MCP；修改 Go 核心代码或安装系统依赖才需要重新部署。

## 可选浏览器自动化

浏览器自动化是增强能力，不属于默认最小部署。默认 `Dockerfile` 不安装 Chromium，也不会暴露 `browser_*` 工具。

Docker 浏览器增强镜像会在构建时自动完成这些步骤：

```text
- 安装 Chromium 系统依赖
- 复制 examples/browser-runner
- 执行 npm install
- 执行 npx playwright install chromium
```

容器启动时会自动创建 AgentDock 子目录，并在 `browser-runner` 不存在时自动初始化。因此 Docker 部署不需要手动执行 `cp`、`npm install` 或 `npx playwright install chromium`。

裸机部署才需要手动安装 runner：

```bash
cd /opt/agentdock
AGENT_DOCK=/srv/coding-agent-dock ./scripts/install-browser-runner.sh
```

### 启用工具

环境变量：

```bash
AGENTDOCK_BROWSER_ENABLED=true
AGENTDOCK_DIR=/srv/coding-agent-dock
```

或启动参数：

```bash
--browser-enabled \
--agentdock-dir /srv/coding-agent-dock
```

重启服务后，工具列表会新增：

```text
browser_session_start
browser_action
browser_snapshot
browser_session_close
```

### Docker 浏览器增强镜像

默认 `Dockerfile` 不安装 Chromium。需要浏览器增强能力时，用 browser compose 覆盖文件即可：

```bash
docker compose -f docker-compose.yml -f docker-compose.browser.yml up -d --build
```

`Dockerfile.browser` 会自动准备 Playwright runner 和 Chromium。容器启动时会自动创建 AgentDock 子目录，并在缺少 runner 时自动初始化。

### 资源影响

浏览器自动化会明显增加资源占用：

```text
存储：通常增加 300MB ~ 800MB，取决于 Chromium、Playwright 和系统依赖。
内存：单个 headless Chromium 会话通常 200MB ~ 500MB，复杂网页可能更高。
```

建议限制并发浏览器会话，并定期清理：

```text
<AgentDock>/browser-artifacts/
```

## 日志

服务使用结构化 JSON 日志，默认输出到 stderr。容器环境下可以直接使用：

```bash
docker logs <container>
docker compose logs -f
```

日志记录内容包括：

- 服务启动参数：workspace、host、port、tool profile、log level。
- HTTP 请求：method、path、status、bytes、duration_ms、remote。
- MCP 工具调用：tool、duration_ms、ok。

日志不会记录：

- Authorization header。
- OAuth code。
- 工具调用参数全文。
- 命令 stdout/stderr 全文。
- token、password、secret。

## GitHub token 配置

可以在 workspace 下准备 `.env`：

```bash
GITHUB_USERNAME=your-github-username
GITHUB_TOKEN=github_pat_xxx
```

然后通过 `configure_github_token` 配置 Git HTTPS credential。该工具只会返回脱敏结果，不会输出 token。

Fine-grained PAT 常见要求：

- 目标仓库需要被 token 授权访问。
- clone/pull 通常需要 `Contents: Read-only`。
- push 通常需要 `Contents: Read and write`。
- 修改 GitHub Actions workflow 需要额外的 `workflow` 权限。

## 安全模型

本项目的安全边界主要包括：

1. **workspace 边界校验**：工具只能访问 workspace 内路径。
2. **命令策略检查**：对 shell expansion、网络访问、敏感命令等进行策略判断。
3. **输出截断与脱敏**：命令和 Git 输出会做字节限制，常见 token 会被脱敏。
4. **Linux Landlock**：在支持的 Linux 内核上，对命令文件系统访问做 best-effort 限制。
5. **容器隔离**：生产或不可信代码场景建议配合 Docker、gVisor、Firecracker 等外部隔离层。

注意：MCP 工具可以读写 workspace，并可执行命令。不要把不可信用户直接接入有敏感凭据的 workspace。

裸机 VPS 如设置 `sandbox-mode=none`，内部 Landlock 和 `no_new_privs` 会被跳过，`sudo` 可以按系统 sudoers 规则提权。该模式只建议在可信私有环境使用，并建议给运行用户配置有限 sudo 权限，而不是 `NOPASSWD: ALL`。

## 开发说明

目录结构：

```text
cmd/agentdock/     CLI 入口
internal/config/          配置与环境变量
internal/auth/            Bearer token 与 OAuth 辅助逻辑
internal/jsonrpc/         JSON-RPC 2.0 类型与响应
internal/mcp/             MCP dispatcher、tool descriptor、tool envelope
internal/httpx/           HTTP transport、OAuth discovery、请求日志
internal/logx/            结构化日志初始化
internal/workspace/       workspace 路径解析与越界保护
internal/policy/          命令策略
internal/session/         长运行命令会话、增量输出、会话停止
internal/sandbox/         Linux Landlock 沙箱；非 Linux 自动降级
internal/tools/           文件、搜索、命令、Git、GitHub、图片等工具实现
```

新增工具的一般步骤：

1. 在 `internal/tools` 中实现 handler。
2. 在 `Runtime.Call` 中注册调用分支。
3. 在 `Runtime.ToolNames` 中加入工具名。
4. 在 `internal/mcp/registry.go` 中补充标题、描述和 annotation。
5. 在 `internal/mcp/schema.go` 中补充 input/output schema。
6. 添加必要测试。
7. 执行：

```bash
gofmt -w $(find . -name '*.go' -type f)
go test ./...
go vet ./...
go build ./...
```

## 排障

### 工具已经更新，但 ChatGPT 侧看不到新工具

先调用：

```text
server_info
```

确认 `tool_count` 和 `tools` 是否包含新工具。如果服务端有但客户端工具列表没有，通常是 MCP 客户端缓存，需要重新连接或刷新 MCP 服务。

也可以调用：

```text
tool_descriptors
```

查看服务端实际暴露的完整 descriptor。

### Git 工具提示不是 Git 仓库

多项目 workspace 下需要传入具体仓库路径：

```json
{
  "repo_path": "agentdock"
}
```

或者先调用：

```text
workspace_repos
```

查看 workspace 下有哪些仓库。

### Git push 权限失败

常见原因：

- GitHub token 没有目标仓库权限。
- Fine-grained PAT 缺少 `Contents: Read and write`。
- 推送 workflow 文件时缺少 `workflow` 权限。
- SSH remote 指向不存在的 deploy key，建议使用 HTTPS remote。

### Docker 构建后仍是旧代码

使用无缓存构建：

```bash
docker compose down
docker compose up -d --build
```

再通过日志确认启动版本和工具数量：

```bash
docker compose logs -f
```

## License

暂未指定许可证。公开分发前建议补充 `LICENSE` 文件。

### macOS launchd 常驻运行

如果希望 AgentDock 在 macOS 登录后自动启动，可以使用 `launchd`。创建：

```bash
nano ~/Library/LaunchAgents/com.uvwt.agentdock.plist
```

示例内容，注意把 `YOUR_USER` 和二进制路径换成自己的：

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>com.uvwt.agentdock</string>
  <key>ProgramArguments</key>
  <array>
    <string>/Users/YOUR_USER/agentdock/agentdock</string>
    <string>--workspace</string>
    <string>/Users/YOUR_USER/agentdock-workspace</string>
    <string>--agentdock-dir</string>
    <string>/Users/YOUR_USER/AgentDock</string>
    <string>--host</string>
    <string>127.0.0.1</string>
    <string>--port</string>
    <string>8765</string>
    <string>--oauth-mode</string>
    <string>--tool-profile</string>
    <string>full</string>
    <string>--sandbox-mode</string>
    <string>none</string>
  </array>
  <key>EnvironmentVariables</key>
  <dict>
    <key>AGENTDOCK_BROWSER_ENABLED</key>
    <string>false</string>
  </dict>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>StandardOutPath</key>
  <string>/tmp/agentdock.out.log</string>
  <key>StandardErrorPath</key>
  <string>/tmp/agentdock.err.log</string>
</dict>
</plist>
```

加载和启动：

```bash
launchctl load ~/Library/LaunchAgents/com.uvwt.agentdock.plist
launchctl start com.uvwt.agentdock
```

停止和卸载：

```bash
launchctl stop com.uvwt.agentdock
launchctl unload ~/Library/LaunchAgents/com.uvwt.agentdock.plist
```
