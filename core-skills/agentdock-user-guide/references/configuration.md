# AgentDock 运行配置速查

本文件描述 AgentDock Core 当前版本从启动环境读取的主要运行配置。它不是新的配置文件格式，也不要求所有发行方式直接暴露这些变量给用户。

## 常用配置

| 配置键 | 作用 | 常见入口 |
|---|---|---|
| `AGENTDOCK_HOST` | Core 监听地址 | Linux env、Docker environment、直接启动 |
| `AGENTDOCK_PORT` | MCP/HTTP 监听端口，默认 8765 | Desktop 设置、Linux env、Docker environment |
| `AGENTDOCK_AUTH_TOKEN` | Bearer Token | 安装器、平台受保护凭据或容器 secret/env |
| `AGENTDOCK_LOG_LEVEL` | `debug` / `info` / `warn` / `error` | Desktop 设置、Linux env、Docker environment |
| `AGENTDOCK_MCP_APPS_ENABLED` | 是否启用 MCP Apps UI，默认启用 | Desktop 设置或启动环境 |
| `AGENTDOCK_BROWSER_ENABLED` | 是否启用浏览器能力 | Desktop 设置或启动环境 |
| `AGENTDOCK_BROWSER_EXECUTABLE_PATH` | 显式浏览器可执行文件 | Docker/服务器/高级运行环境 |
| `AGENTDOCK_BROWSER_CDP_URL` | 复用已有 Chromium 的 CDP 地址 | Desktop 设置或启动环境 |
| `AGENTDOCK_BROWSER_REUSE_EXISTING_CDP` | 自动复用唯一已发现 CDP | Desktop 设置或启动环境 |
| `AGENTDOCK_COMMAND_ENV_FROM_ENV_JSON` | 显式允许 `exec_command` 从 Core 宿主环境复制的变量映射 | Linux/Docker/直接启动的高级配置 |
| `AGENTDOCK_ACP_ENABLED` | 是否启用 ACP Client | Desktop 设置或启动环境 |
| `AGENTDOCK_ACP_AGENT` | ACP Agent 预设/名称 | Desktop 设置或启动环境 |
| `AGENTDOCK_ACP_COMMAND` | ACP Adapter 命令 | 自定义/高级 ACP 配置 |
| `AGENTDOCK_ACP_ARGS_JSON` | ACP Adapter 参数 JSON 数组 | 自定义/高级 ACP 配置 |
| `AGENTDOCK_ACP_ENV_FROM_ENV_JSON` | ACP 子进程环境映射 | 高级 ACP 配置 |
| `AGENTDOCK_ACP_MAX_CONCURRENT_PROMPTS` | ACP 并发 prompt 上限 | 高级 ACP 配置 |
| `AGENTDOCK_ACP_INTERACTION_TIMEOUT_MS` | ACP 交互超时 | 高级 ACP 配置 |
| `AGENTDOCK_SERVER_URL` | 对外服务 Origin | 公网/OAuth 安装流程 |
| `AGENTDOCK_OAUTH_ENABLED` | 是否启用 OAuth | 安装器/公网访问配置 |
| `AGENTDOCK_OAUTH_PASSWORD` | OAuth 登录密码 | 平台安全存储或受保护环境 |
| `AGENTDOCK_OAUTH_TOKEN_SECRET` | OAuth Token 签名密钥 | 平台安全存储或受保护环境 |
| `AGENTDOCK_OAUTH_ACCESS_TOKEN_TTL` | OAuth Access Token 有效期 | Desktop/高级启动配置 |
| `AGENTDOCK_STDIO` | 是否启用 stdio 运行模式 | 直接启动/集成场景 |
| `AGENTDOCK_TRUSTED_PROXY_CIDRS` | 受信任反向代理网段 | 服务器/反代场景 |
| `AGENTDOCK_INSTRUCTIONS_FILE` | 额外 Instructions 文件 | 高级启动配置 |

## 重要边界

- Windows Desktop 不应把认证秘密直接写入 `control-panel-settings.json`。Bearer Token、OAuth 密码、OAuth 签名密钥和 Tunnel Token 使用平台受保护存储。
- macOS Desktop 的 `agentdock.env` 包含运行所需配置，可能含秘密；文件必须保持仅当前用户可读写。
- Linux 官方安装器默认把环境文件按 root:root、0600 写入，并通过 systemd/OpenRC 注入服务进程；不要为了方便把权限放宽。
- Docker 的环境变量属于容器创建配置。Compose 文件或 env file 修改后，如果容器没有被重新创建，新进程可能仍使用旧的容器配置。
- `AGENTDOCK_COMMAND_ENV_FROM_ENV_JSON` 只允许显式映射；它不会自动把登录 Shell 的全部环境传给 `exec_command`。

## 判断“配置没生效”时

按顺序检查：

1. 修改的是不是实际启动来源；
2. Core 是否真的重新启动/容器是否真的重新创建；
3. 新进程是否健康；
4. 目标配置对应的行为是否变化；
5. 是否有更高层的 Desktop、service、Compose 或进程管理器重新覆盖了手工修改。
