# 将 AgentDock 接入 Hermes Agent

Hermes Agent 内置 MCP 客户端，因此可以不通过浏览器桥接、也不需要 OpenAI API Key 使用 AgentDock。根据 Hermes 与 AgentDock 的部署位置选择传输方式：

- **本机 stdio**：Hermes 和 AgentDock 在同一台设备上时的推荐方式，不需要公网地址或 Token。
- **Streamable HTTP**：Hermes 连接另一台设备、容器或公网隧道中的 AgentDock 时使用，必须保留 Bearer 或 OAuth 认证。

## 本机 stdio（推荐）

通过 Hermes 启动 AgentDock，不需要监听网络端口：

```bash
hermes mcp add agentdock \
  --command /absolute/path/to/agentdock \
  --args --stdio
```

`hermes mcp add` 会先连接并发现工具，然后询问要启用哪些工具。`--args` 必须放在最后，因为它后面的参数都会传给 AgentDock。

如果要固定 AgentDock 的工作目录，可以在 stdio Server 环境中指定绝对路径：

```yaml
# ~/.hermes/config.yaml
mcp_servers:
  agentdock:
    command: "/absolute/path/to/agentdock"
    args: ["--stdio"]
    env:
      AGENTDOCK_DEFAULT_DIR: "/absolute/path/to/workspace"
    connect_timeout: 60
    timeout: 180
```

`AGENTDOCK_DEFAULT_DIR` 是可选的。省略时，AgentDock 使用当前用户的默认目录。

本机 stdio 不需要 `AGENTDOCK_AUTH_TOKEN` 或 OAuth。Hermes 负责管理子进程管道，不要为了这条连接单独配置公网隧道。

## Streamable HTTP

当 Hermes 与 AgentDock 位于不同运行环境时，使用公网或本地 HTTP MCP 地址：

```yaml
# ~/.hermes/config.yaml
mcp_servers:
  agentdock:
    url: "https://agent.example.com/mcp"
    headers:
      Authorization: "Bearer ${MCP_AGENTDOCK_API_KEY}"
    connect_timeout: 60
    timeout: 180
```

把 Token 放在 Hermes 当前 Profile 的环境文件中，不要写入 `config.yaml`：

```text
# ~/.hermes/.env
MCP_AGENTDOCK_API_KEY=<the AgentDock Bearer token>
```

使用 `--profile` 运行 Hermes 时，应使用对应 Profile 的实际目录。不要把真实 Token 提交到仓库，或粘贴到 Issue、日志、截图和公开聊天中。URL 必须包含 `/mcp`，公网部署必须使用 HTTPS。

如果 AgentDock 端点配置为 OAuth，可以使用 Hermes 的发现流程：

```bash
hermes mcp add agentdock \
  --url "https://agent.example.com/mcp" \
  --auth oauth
```

按照浏览器授权提示操作。不要把 OAuth 密码、Tunnel Token 或 Access Token 放进 MCP URL。

## 验证连接

在同一个 Hermes Profile 下执行：

```bash
hermes mcp list
hermes mcp test agentdock
```

成功时应能看到 AgentDock 的传输方式和大于零的工具发现数量。修改 `mcp_servers` 后请新开 Hermes 会话，因为 MCP 工具会在 Hermes 初始化 Server 连接时发现。

行为验证时，让 Hermes 调用 `agentdock_context`，确认返回的运行时、操作系统、默认目录和能力信息；然后在配置的工作目录中执行一次无副作用的只读操作。

## 边界与排障

- `--stdio` 启动的 AgentDock 继承 Hermes 进程的操作系统权限。建议将 `AGENTDOCK_DEFAULT_DIR` 指向专用工作区，并把秘密文件放在工作区之外。
- HTTP 连接必须使用完整的 MCP 端点（`/mcp`），不能只填公网 Origin 或健康检查 URL。
- 临时公网地址可能在隧道重启后变化；地址变化后要更新 Hermes 配置并重新授权。
- 如果工具缺失，运行 `hermes mcp test agentdock`，检查 Hermes 是否保存了 `tools.include` 过滤器，然后新开会话。
- 如果连接失败，先检查 AgentDock 进程或 `/healthz`，再查看 Hermes 的 MCP 启动输出。配置文件写入成功不等于客户端已经连接。
