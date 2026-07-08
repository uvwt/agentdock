# Docker 部署

Docker 适合文件、Git、原生 Skill、浏览器自动化等能力。macOS 桌面自动化需要裸机部署，不能在容器里控制宿主机桌面。

AgentDock 使用单一 Host 路径模型：

```text
~/.agentdock   内部状态目录：tasks、skills、nexus、env、browser artifacts、private notes
~/AgentDock    默认工作目录：文件、命令、Git 工具的默认起点
```

在 Docker 内部默认对应 `/root/.agentdock` 和 `/root/AgentDock`。容器内可见文件范围由 Docker volume 决定；AgentDock 不把默认工作目录当强安全边界。

## Compose 示例

```yaml
services:
  agentdock:
    image: agentdock:local
    ports:
      - "127.0.0.1:18766:8765"
    volumes:
      - ./AgentDockHome:/root/.agentdock
      - ./AgentDock:/root/AgentDock
    environment:
      AGENTDOCK_AUTH_TOKEN: "replace-with-a-local-secret"
    command:
      - agentdock
      - --host
      - 0.0.0.0
      - --port
      - "8765"
```

## 快速启动

```bash
make docker-build
make docker-up
make smoke-docker
```

默认 MCP 入口：

```text
http://127.0.0.1:18766/mcp
```

## 浏览器镜像

`make docker-browser-build` 使用 `Dockerfile.browser` 构建 `agentdock:browser`。浏览器增强镜像包含 `/opt/agentdock/browser-runner`；启动时，如果 `~/.agentdock/browser-runner/` 还不存在，entrypoint 会自动复制 runner 到内部状态目录。

## 数据清理

如果需要删除运行数据，手动删除 compose 目录里的 `AgentDockHome/` 和 `AgentDock/`，并确认里面没有需要保留的项目、Skill 数据或 artifacts。
