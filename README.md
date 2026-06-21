# AgentDock

AgentDock 是本地/远程 Agent 工具运行层，提供文件、命令、Git、原生 Skill、浏览器自动化、macOS 桌面自动化和 RecallDock 长期召回能力。

## 核心能力

- 工作区文件读取、搜索、结构化补丁。
- 受控命令执行、会话管理、输出截断和脱敏。
- 内置可恢复任务状态 `task_manage`，用于多步骤闭环任务的阶段、完成条件和证据持久化。
- Git / GitHub 仓库操作。
- 原生 Skill `skill_manage`，支持预检、安装、检查、运行与回滚。
- 可选浏览器自动化 `browser_*`。
- macOS 裸机桌面自动化 `desktop_*`。
- RecallDock 长期召回 `recall_*`。

## 快速验证开发环境

```bash
make check
```

## Docker Quickstart

本机或受信环境中，最快路径是 Docker Compose：

```bash
make docker-build
make docker-up
make smoke-docker
```

默认 MCP 入口：

```text
http://127.0.0.1:18766/mcp
```

查看日志和停止服务：

```bash
make logs
make docker-down
```

Docker quickstart 使用 localhost demo 配置。公网或长期运行请阅读 [VPS systemd 部署](docs/install-vps-systemd.md)，配置鉴权和反代后再开放访问。

需要浏览器自动化时，先运行 `make docker-browser-build`，再按 [Docker 部署](docs/install-docker.md) 使用 browser overlay。

## Linux 一键部署

Linux 服务器推荐使用问答式 systemd 安装脚本：

```bash
bash scripts/install-linux.sh
```

脚本会按提示填写源码目录、运行目录、监听端口、Bearer token、RecallDock/Nexus 可选配置，并自动构建、写入 systemd unit、启动和 smoke 验证。单文件远程安装见 [Linux 问答式一键部署](docs/install-linux-interactive.md)。

## macOS 裸机更新

```bash
cd ~/agentdock
make check
make install-macos
make restart-macos
make smoke-macos
```

## 文档

- [macOS 裸机 launchd 部署](docs/install-macos-launchd.md)
- [Docker 部署](docs/install-docker.md)
- [VPS systemd 部署](docs/install-vps-systemd.md)
- [Linux 问答式一键部署](docs/install-linux-interactive.md)
- [macOS desktop 自动化](docs/desktop-automation.md)
- [RecallDock](docs/recalldock.md)
- [可恢复任务状态](docs/tasks.md)
- [安全模型](docs/security.md)
- [排障](docs/troubleshooting.md)
- [开发与质量门禁](docs/development.md)

## 开发约定

- `main` 是稳定主分支。
- 合并前运行 `make check`。
- Git commit message 使用中文。
- README 只保留入口和原则，细节放入 `docs/`。
- macOS 桌面自动化必须裸机运行；Docker 不提供真实桌面控制。

## License

MIT
