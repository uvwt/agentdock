# Docker 部署

Docker 适合文件、Git、原生 Skill、浏览器自动化等能力。macOS 桌面自动化必须使用裸机部署，不能在容器里控制宿主机桌面。

## Quickstart

```bash
git clone <repo-url> agentdock
cd agentdock

make docker-build
make docker-up
make smoke-docker
```

默认本机入口：

```text
MCP:     http://127.0.0.1:18766/mcp
Health:  http://127.0.0.1:18766/healthz
```

查看日志或停止服务：

```bash
make logs
make docker-down
```

## 目录和持久化

默认 Compose 会在仓库目录下创建两个持久化目录：

```text
workspace/    用户项目工作区，Agent 工具读写这里
AgentDock/    AgentDock 控制目录，保存 state、cache、Skill、browser artifacts 等运行数据
```

这两个目录是运行数据，不应提交到 Git。迁移服务时同时迁移这两个目录，或改成明确的宿主机绝对路径/volume。

## Demo 权限模式

默认 `docker-compose.yml` 绑定本机端口：

```text
127.0.0.1:18766 -> container 8765
```

并带有：

```text
--dangerously-skip-all-permissions
```

这个配置只适合 localhost 或受信 demo 环境，用来降低首次体验门槛。不要把这个默认配置直接暴露到公网。

公网或长期运行时应：

1. 移除 `--dangerously-skip-all-permissions`。
2. 设置 `AGENTDOCK_AUTH_TOKEN`，并只把反代入口暴露给外部。
3. 通过 HTTPS 反代访问 `/mcp`。
4. 用 smoke 命令带 token 验证 MCP：

```bash
AGENTDOCK_SMOKE_URL=https://agentdock.example.com \
AGENTDOCK_AUTH_TOKEN="$AGENTDOCK_AUTH_TOKEN" \
make smoke-docker
```

## Smoke 验证

`make smoke-docker` 会验证：

- `/healthz` 返回 `ok=true`。
- `/mcp` 能完成 `initialize`。
- `tools/list` 能返回工具列表。
- `tools/call server_info` 能返回结构化运行信息。

默认目标是：

```text
http://127.0.0.1:18766
```

可通过环境变量覆盖：

```bash
AGENTDOCK_SMOKE_URL=http://127.0.0.1:8765 make smoke-docker
AGENTDOCK_SMOKE_TIMEOUT_SECONDS=10 make smoke-docker
AGENTDOCK_SMOKE_ATTEMPTS=20 make smoke-docker
```

如果服务启用了 bearer token：

```bash
AGENTDOCK_AUTH_TOKEN="<token>" make smoke-docker
```

不要把真实 token 写入 README、compose 文件或 shell history 中可共享的位置。

## 浏览器增强

基础 Docker 部署不会默认启用浏览器能力。浏览器增强使用 overlay compose：

```bash
make docker-browser-build
docker compose -f docker-compose.yml -f docker-compose.browser.yml up -d
make smoke-docker
```

`make docker-browser-build` 使用 `Dockerfile.browser` 构建 `agentdock:browser`。浏览器增强镜像包含 `/opt/agentdock/browser-runner`；启动时，如果 `AgentDock/browser-runner/` 还不存在，entrypoint 会自动复制 runner 到 AgentDock 控制目录。

启用后用 `server_info` 或 `tools/list` 确认 `browser_enabled=true` 和 `browser_*` 工具可见。

## 常见操作

重新构建并重启：

```bash
make docker-build
make docker-up
make smoke-docker
```

查看容器状态：

```bash
docker compose ps
docker compose logs --tail=100 agentdock
```

清理容器但保留运行数据：

```bash
make docker-down
```

如果需要删除运行数据，手动删除 `workspace/` 和 `AgentDock/`，并确认里面没有需要保留的项目、Skill 数据或 artifacts。
