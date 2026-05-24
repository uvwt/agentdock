# Coding Tools MCP Go

Coding Tools MCP Go 是一个用 Go 编写的 Model Context Protocol（MCP）工具服务。它把一个本地或容器内的工作空间暴露给支持 MCP 的客户端，让模型可以安全地读取文件、搜索代码、编辑文件、执行受控命令、管理长任务会话、处理 Git 仓库，并查看图片资源。

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

## 多项目 workspace 模型

本服务把 `workspace` 当作总工作区，而不是默认把 workspace 本身当成 Git 仓库。

推荐结构：

```text
/workspace/
  coding-tools-mcp-go/
    .git/
  another-project/
    .git/
```

Git 工具建议显式传入：

```json
{
  "repo_path": "coding-tools-mcp-go"
}
```

这样可以避免多项目场景下误操作到错误仓库。

示例：

```json
{
  "repo_path": "coding-tools-mcp-go",
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
go build ./cmd/coding-tools-mcp
```

运行二进制：

```bash
./coding-tools-mcp --workspace /path/to/workspace --host 127.0.0.1 --port 8765
```

HTTP MCP endpoint：

```text
http://127.0.0.1:8765/mcp
```

stdio 模式：

```bash
./coding-tools-mcp --stdio --workspace /path/to/workspace
```

## Docker 使用

本地构建镜像：

```bash
docker build -t coding-tools-mcp-go:local .
```

使用 Docker Compose 构建并启动：

```bash
docker compose build --no-cache
docker compose up -d
```

查看状态和日志：

```bash
docker compose ps
docker compose logs -f
```

代码更新后的推荐流程：

```bash
cd coding-tools-mcp-go

go test ./...

docker compose down
docker compose build --no-cache
docker compose up -d

docker compose logs -f
```

如果只想手动构建镜像：

```bash
docker build --no-cache -t coding-tools-mcp-go:local .
```

## 配置项

配置可以通过环境变量或命令行参数传入。

| 环境变量 | 默认值 | 说明 |
| --- | --- | --- |
| `CODING_TOOLS_MCP_WORKSPACE` | `.` | workspace 根目录。 |
| `CODING_TOOLS_MCP_HOST` | `127.0.0.1` | HTTP 监听地址。 |
| `CODING_TOOLS_MCP_PORT` | `8765` | HTTP 监听端口。 |
| `CODING_TOOLS_MCP_AUTH_TOKEN` | 空 | 可选 Bearer token。 |
| `CODING_TOOLS_MCP_TOOL_PROFILE` | `full` | 工具集配置，支持 `full`、`read-only`、`compat-readonly-all`。 |
| `CODING_TOOLS_MCP_ENABLE_VIEW_IMAGE` | `true` | 是否暴露 `view_image` 工具。 |
| `CODING_TOOLS_MCP_STDIO` | `false` | 是否使用 stdio 模式。 |
| `CODING_TOOLS_MCP_SKIP_PERMISSION_PROMPTS` | `false` | 是否跳过命令策略确认。仅建议在受信容器中使用。 |
| `CODING_TOOLS_MCP_LOG_LEVEL` | `info` | 日志级别：`debug`、`info`、`warn`、`error`。 |

常用命令行参数：

```bash
./coding-tools-mcp \
  --workspace /workspace \
  --host 0.0.0.0 \
  --port 8765 \
  --log-level info
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

## 开发说明

目录结构：

```text
cmd/coding-tools-mcp/     CLI 入口
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
  "repo_path": "coding-tools-mcp-go"
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
docker compose build --no-cache
docker compose up -d
```

再通过日志确认启动版本和工具数量：

```bash
docker compose logs -f
```

## License

暂未指定许可证。公开分发前建议补充 `LICENSE` 文件。
