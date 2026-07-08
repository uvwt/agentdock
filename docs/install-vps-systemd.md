# VPS systemd 部署

VPS 适合长期运行 AgentDock 远程 MCP 入口，并通过 HTTPS 反代暴露给外部 Agent。VPS 不具备 macOS 桌面自动化能力，不建议安装或运行 `desktop` Skill。

## 目标拓扑

```text
client -> HTTPS reverse proxy -> 127.0.0.1:8765 -> agentdock
```

建议只让 AgentDock 监听本机地址，由 Caddy、Nginx 或其他反代负责 TLS、域名和公网入口。

## 目录约定

AgentDock 使用单一 Host 路径模型，目录固定来自运行用户 home：

```text
~/.agentdock   AgentDock 内部状态目录：tasks、skills、nexus、env、browser artifacts、private notes
~/AgentDock    默认工作目录：文件、命令、Git 工具的默认起点
```

在 systemd 里如果使用 `User=agentdock`，则这两个目录位于该用户的 home 下，例如 `/srv/agentdock/.agentdock` 和 `/srv/agentdock/AgentDock`。AgentDock 不把 `~/AgentDock` 当强安全边界；实际文件范围由运行用户权限、systemd 配置和挂载目录决定。

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
AGENTDOCK_HOST=127.0.0.1
AGENTDOCK_PORT=8765
AGENTDOCK_AUTH_TOKEN=<replace-with-a-secret>
AGENTDOCK_LOG_LEVEL=info
```

公网或共享入口建议配置 `AGENTDOCK_AUTH_TOKEN`，客户端访问 `/mcp` 时使用 bearer token。不要把 token、私钥或真实域名凭据写进仓库文档。

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
  --host ${AGENTDOCK_HOST} \
  --port ${AGENTDOCK_PORT} \
  --log-level ${AGENTDOCK_LOG_LEVEL}
Restart=on-failure
RestartSec=3

[Install]
WantedBy=multi-user.target
```

建议：

- 公网场景不要默认使用 `--dangerously-skip-all-permissions`。
- `User=agentdock` 建议只拥有需要操作的项目目录权限。
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

建议反代不记录 Authorization header。公网环境建议使用 HTTPS，并限制只有需要访问 MCP 的客户端知道 bearer token。

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
