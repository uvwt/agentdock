# 排障

## 工具已经更新，但 ChatGPT 侧看不到

1. 确认实际运行二进制来自 `$HOME/agentdock/agentdock`。
2. 执行：

```bash
cd ~/agentdock
make install-macos
make restart-macos
```

3. 检查：

```bash
curl -fsS http://127.0.0.1:18766/healthz
tail -n 100 ~/agentdock-runtime/agentdock.err.log
```

## 桌面操作返回 ok=true 但 UI 没变

`ok=true` 只代表命令发出。请使用：

```json
{"verify": true, "wait_ms": 300}
```

并检查 `effect_verified`、`effect_changed`、`error_layer`。

## Git push 权限失败

检查 remote、credential helper 和 GitHub token，不要在 README 或日志中记录私有 token。

## Docker 构建后仍是旧代码

确认 Compose 使用的是新镜像，必要时：

```bash
docker compose build --no-cache
docker compose up -d
```

然后运行：

```bash
make smoke-docker
```

## Docker smoke 失败

先确认服务和端口：

```bash
docker compose ps
docker compose logs --tail=100 agentdock
curl -fsS http://127.0.0.1:18766/healthz
```

常见失败层级：

- `GET /healthz failed`：容器未启动、端口映射不一致，或 `AGENTDOCK_SMOKE_URL` 指错。
- `HTTP 401`：服务启用了 bearer token，但 smoke 没有带同一个 `AGENTDOCK_AUTH_TOKEN`。
- `MCP initialize returned non-JSON response`：反代或端口指向的不是 AgentDock `/mcp`。
- `server_info not exposed`：当前服务不是预期的 AgentDock 版本，或工具 profile 配置异常。

带 token 验证：

```bash
AGENTDOCK_AUTH_TOKEN="<token>" make smoke-docker
```

指定非默认端口：

```bash
AGENTDOCK_SMOKE_URL=http://127.0.0.1:8765 make smoke-docker
```

## VPS healthz 正常但 MCP 不可用

`/healthz` 不需要鉴权，只能证明进程活着；还必须验证 `/mcp`：

```bash
AGENTDOCK_SMOKE_URL=http://127.0.0.1:8765 \
AGENTDOCK_AUTH_TOKEN="<token>" \
make smoke-docker
```

如果本机 smoke 通过但公网失败，检查：

- 反代是否把 `/mcp` 转发到 `127.0.0.1:8765`。
- HTTPS 证书和域名是否生效。
- 反代是否丢弃或覆盖了 Authorization header。
- 客户端配置的 bearer token 是否和 `/etc/agentdock/agentdock.env` 一致。
