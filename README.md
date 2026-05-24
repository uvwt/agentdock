# Coding Tools MCP Go

一个独立的 Go 重构版 Coding Tools MCP Server。

设计目标：

- 保持 MCP 工具协议兼容。
- 用小包拆分职责，方便维护。
- 默认只绑定一个 workspace，所有文件路径都经过 workspace 边界校验。
- HTTP 与 stdio 两种传输层共享同一套 JSON-RPC dispatcher。
- 工具实现与协议层解耦，后续新增工具只需要注册 handler。
- 尽量使用标准库，降低维护成本。
- 工具元数据集中在 registry 中，避免 schema、description、annotation 分散维护。

## 目录结构

```text
cmd/coding-tools-mcp/     CLI 入口
internal/config/          配置与环境变量
internal/auth/            HTTP 鉴权
internal/jsonrpc/         JSON-RPC 2.0 类型与响应
internal/mcp/             MCP dispatcher、tool descriptors、envelope
internal/httpx/           HTTP transport
internal/workspace/       workspace 路径安全
internal/policy/          命令风险策略
internal/session/         长运行命令会话
internal/sandbox/         Linux Landlock 沙箱；非 Linux 自动降级并返回 warning
internal/tools/           MCP 工具实现
```

## 构建

```bash
go test ./...
go build ./cmd/coding-tools-mcp
```

## 运行

```bash
./coding-tools-mcp --workspace /path/to/repo --host 127.0.0.1 --port 8765
```

HTTP endpoint:

```text
http://127.0.0.1:8765/mcp
```

Stdio:

```bash
./coding-tools-mcp --stdio --workspace /path/to/repo
```

## Docker

本地构建镜像：

```bash
docker build -t coding-tools-mcp-go:local .
```

如果更新了代码，建议按下面顺序重新构建并启动：

```bash
# 1. 进入项目目录
cd coding-tools-mcp-go

# 2. 可选：先跑测试，确认代码可以通过编译与基础检查
go test ./...

# 3. 重新构建本地镜像
docker build -t coding-tools-mcp-go:local .

# 4. 停掉旧容器
docker compose down

# 5. 用新镜像重新启动服务
docker compose up -d

# 6. 查看容器状态和日志
docker compose ps
docker compose logs -f
```

如果怀疑 Docker 缓存导致旧代码没有被打进镜像，可以使用无缓存构建：

```bash
docker build --no-cache -t coding-tools-mcp-go:local .
```

如果使用 Docker Compose 统一构建和启动：

```bash
docker compose build --no-cache
docker compose up -d
```

## 当前迁移状态

已覆盖核心工具：

- `server_info`
- `get_default_cwd`
- `set_default_cwd`
- `read_file`
- `list_dir`
- `list_files`
- `search_text`
- `apply_patch`
- `exec_command`
- `write_stdin`
- `kill_session`
- `git_status`
- `git_diff`
- `git_log`
- `git_show`
- `git_blame`
- `request_permissions`
- `view_image`

补齐能力：

- OAuth authorization code + PKCE 主流程。
- Bearer token 与 OAuth token 校验。
- MCP server-card 与 OAuth discovery metadata。
- `exec_command` 增量 stdout/stderr cursor 与输出统计字段。
- `list_dir` / `list_files` / `search_text` 支持 `.gitignore`、glob 与 include/exclude 参数。
- `view_image` 支持尺寸识别、`max_bytes`、`max_width`、`max_height`、`auto_resize`、`data_url` 与双线性缩放。
- `apply_patch` 支持 envelope patch、dry-run、move、BOM/CRLF 保留。

Linux 下 `exec_command` 会 best-effort 启用 Landlock 文件系统限制；非 Linux 或内核不支持时会在 `server_info.sandbox` / `exec_command.sandbox` 中返回 warning。生产或不可信 workspace 场景仍建议放在 Docker、gVisor、Firecracker 等外部沙箱里运行。

