# VPS systemd 部署

VPS 适合长期运行 AgentDock 远程 MCP 入口，并通过 HTTPS 反代暴露给外部 Agent。VPS 不具备 macOS 桌面自动化能力，不应启用 `desktop_*`。

## 目标拓扑

```text
client -> HTTPS reverse proxy -> 127.0.0.1:8765 -> agentdock
```

推荐只让 AgentDock 监听本机地址，由 Caddy、Nginx 或其他反代负责 TLS、域名和公网入口。

## 目录约定

以下路径是示例，可按服务器习惯调整：

```text
/opt/agentdock                 AgentDock 源码
/srv/agentdock/workspace       用户项目 workspace
/srv/agentdock/AgentDock       AgentDock 控制目录和运行状态
/etc/agentdock/agentdock.env   环境变量文件
```

不要把 token、私钥或真实域名凭据写进仓库文档。

## 构建

```bash
cd /opt/agentdock
git pull --ff-only
make check
go build -trimpath -o ./bin/agentdock ./cmd/agentdock
```

## 环境变量

创建 `/etc/agentdock/agentdock.env`：

```bash
AGENTDOCK_WORKSPACE=/srv/agentdock/workspace
AGENTDOCK_DIR=/srv/agentdock/AgentDock
AGENTDOCK_HOST=127.0.0.1
AGENTDOCK_PORT=8765
AGENTDOCK_TOOL_PROFILE=unified
AGENTDOCK_AUTH_TOKEN=<replace-with-a-secret>
AGENTDOCK_LOG_LEVEL=info
```

公网 production path 必须配置鉴权。最简单的方式是设置 `AGENTDOCK_AUTH_TOKEN`，客户端访问 `/mcp` 时使用 bearer token。

## systemd unit

创建 `/etc/systemd/system/agentdock.service`：

```ini
[Unit]
Description=AgentDock MCP server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=agentdock
Group=agentdock
WorkingDirectory=/opt/agentdock
EnvironmentFile=/etc/agentdock/agentdock.env
ExecStart=/opt/agentdock/bin/agentdock \
  --workspace ${AGENTDOCK_WORKSPACE} \
  --agentdock-dir ${AGENTDOCK_DIR} \
  --host ${AGENTDOCK_HOST} \
  --port ${AGENTDOCK_PORT}
Restart=on-failure
RestartSec=3

[Install]
WantedBy=multi-user.target
```

说明：

- production 默认不使用 `--dangerously-skip-all-permissions`。
- `User=agentdock` 应只拥有 `/srv/agentdock` 和必要项目目录权限。
- 如果用环境变量传 `AGENTDOCK_AUTH_TOKEN`，不要把 env 文件提交到仓库。

启动：

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now agentdock
```

## 反代

Caddy 示例：

```caddyfile
agentdock.example.com {
  reverse_proxy 127.0.0.1:8765
}
```

反代不应记录 Authorization header。生产环境应使用 HTTPS，并限制只有需要访问 MCP 的客户端知道 bearer token。

## 验证

本机服务检查：

```bash
systemctl status agentdock --no-pager
journalctl -u agentdock -n 100 --no-pager
curl -fsS http://127.0.0.1:8765/healthz
```

MCP smoke：

```bash
cd /opt/agentdock
AGENTDOCK_SMOKE_URL=http://127.0.0.1:8765 \
AGENTDOCK_AUTH_TOKEN="$(. /etc/agentdock/agentdock.env; printf '%s' "$AGENTDOCK_AUTH_TOKEN")" \
make smoke-docker
```

公网入口验证：

```bash
AGENTDOCK_SMOKE_URL=https://agentdock.example.com \
AGENTDOCK_AUTH_TOKEN="$(. /etc/agentdock/agentdock.env; printf '%s' "$AGENTDOCK_AUTH_TOKEN")" \
make smoke-docker
```

## 升级

```bash
cd /opt/agentdock
git pull --ff-only
make check
go build -trimpath -o ./bin/agentdock ./cmd/agentdock
sudo systemctl restart agentdock
AGENTDOCK_SMOKE_URL=http://127.0.0.1:8765 \
AGENTDOCK_AUTH_TOKEN="$(. /etc/agentdock/agentdock.env; printf '%s' "$AGENTDOCK_AUTH_TOKEN")" \
make smoke-docker
```

如果 smoke 失败，不要只看 healthz。继续检查：

- `journalctl -u agentdock -n 100 --no-pager`
- 反代日志
- `AGENTDOCK_AUTH_TOKEN` 是否和客户端一致
- `/mcp` 是否返回 401、405 或 JSON-RPC 错误

## 回滚

保留上一版二进制或 Git commit。回滚时先恢复二进制或 checkout 到上一版，再重启和 smoke：

```bash
sudo systemctl restart agentdock
AGENTDOCK_SMOKE_URL=http://127.0.0.1:8765 \
AGENTDOCK_AUTH_TOKEN="$(. /etc/agentdock/agentdock.env; printf '%s' "$AGENTDOCK_AUTH_TOKEN")" \
make smoke-docker
```
