# Docker / Docker Compose 配置与生效

适用于官方 AgentDock Docker 镜像或基于它的 Compose 部署。宿主机可能是 macOS、Windows 或 Linux，但配置事实源是**容器创建配置**，不是宿主操作系统的 Desktop 配置文件。

## 配置事实源

官方镜像中 Core 直接读取容器进程环境。常见来源包括：

- Compose `environment:`；
- Compose `env_file:`；
- `docker run -e/--env-file`；
- 编排平台提供的环境变量或 secret 注入。

官方 runtime 镜像默认：

```text
HOME=/home/agentdock
AGENTDOCK_HOST=0.0.0.0
AGENTDOCK_PORT=8765
```

官方镜像的容器用户 HOME 默认为 `/home/agentdock`，工作目录遵循 `$HOME/AgentDock`。如果派生镜像改变 HOME，应以容器真实 HOME 为准，而不是硬编码用户目录。

AgentDock 状态默认位于容器用户 HOME 下。生产部署通常应把需要持久化的状态通过 volume/bind mount 保存；不要把“修改正在运行容器里的临时文件”当成长期配置方案。

## 修改方式

优先修改真正创建容器的 Compose/编排配置，而不是进入容器后临时 `export`。

Compose 示例：

```yaml
services:
  agentdock:
    environment:
      AGENTDOCK_PORT: "8765"
      AGENTDOCK_LOG_LEVEL: info
      AGENTDOCK_MCP_APPS_ENABLED: "true"
```

认证 Token、OAuth 密码和签名密钥不应提交到公开 Compose 文件；使用部署环境的 secret/env 管理方式，并避免在 `docker inspect`、日志或回复中回显真实值。

## 生效：不要只 restart

Compose 文件、env file 或环境变量变化后，旧容器的创建配置不会因为 `docker compose restart` 自动改变。

应让 Compose 根据新配置重新创建容器：

```bash
docker compose up -d
```

如果需要明确保证重建：

```bash
docker compose up -d --force-recreate
```

`docker compose restart` 只重启现有容器，不能作为“环境变量已更新”的验证手段。

使用 `docker run` 的部署则需要删除/替换旧容器，并用新的 `-e`、`--env-file`、volume 和端口映射重新创建。

## 端口变化要检查两层

`AGENTDOCK_PORT` 改变的是**容器内 Core 监听端口**。如果宿主端口映射仍是旧值，外部访问仍会失败。

例如 Core 改到 9876 时，Compose 也应同步检查：

```yaml
ports:
  - "9876:9876"
```

不要只改一侧。

## 浏览器镜像

官方 Dockerfile 的 browser 变体会启用浏览器并把 Chromium 路径配置为 `/usr/bin/chromium`。普通 runtime 镜像没有同样的浏览器依赖保证。

用户询问“为什么 Docker 里启用 browser 仍不可用”时，先确认实际镜像变体和容器内浏览器可执行文件，不要只把 `AGENTDOCK_BROWSER_ENABLED=true` 作为充分条件。

## 验证

先看容器是否基于新配置被重新创建，再检查：

```bash
docker compose ps
docker compose logs --tail=100 agentdock
```

然后从正确网络位置访问实际端口的：

```text
/healthz
```

最后验证本次修改对应的功能。不要在验证输出中打印完整容器环境，因为其中可能包含认证秘密。
