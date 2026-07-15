<div align="center">

# AgentDock

**A production-oriented tool runtime for local and remote AI agents.**

为 AI Agent 提供统一、可扩展、可审计的文件、命令、Git、Skill、MCP、浏览器自动化与任务执行能力。

[在线文档](https://uvwt.github.io/agentdock-docs/) · [快速开始](https://uvwt.github.io/agentdock-docs/docs/getting-started/docker) · [版本发布](https://github.com/uvwt/agentdock/releases) · [问题反馈](https://github.com/uvwt/agentdock/issues)

[![CI](https://github.com/uvwt/agentdock/actions/workflows/ci.yml/badge.svg)](https://github.com/uvwt/agentdock/actions/workflows/ci.yml)
[![GitHub Release](https://img.shields.io/github/v/release/uvwt/agentdock?display_name=tag&logo=github)](https://github.com/uvwt/agentdock/releases)
[![Docker Hub](https://img.shields.io/docker/pulls/agentdockio/agentdock?logo=docker&label=Docker%20Hub)](https://hub.docker.com/r/agentdockio/agentdock)
[![GHCR](https://img.shields.io/badge/GHCR-ghcr.io%2Fuvwt%2Fagentdock-2496ED?logo=docker&logoColor=white)](https://github.com/uvwt/agentdock/pkgs/container/agentdock)

</div>

---

## 项目简介

AgentDock 是一个面向 AI Agent 的独立工具运行层。它将主机上的文件系统、命令执行、Git、Skill、动态 MCP、浏览器自动化和可恢复任务封装为统一的 MCP 能力，并为本地电脑、远程服务器与容器环境提供一致的运行方式。

AgentDock 不负责聊天界面或模型推理。它专注于让 Agent 以明确边界和可验证结果操作真实环境。

```text
AI Client / Agent
        │
        │ MCP
        ▼
┌───────────────────────────┐
│         AgentDock         │
│                           │
│ Files · Commands · Git    │
│ Skills · Dynamic MCP      │
│ Browser · Tasks · Recall  │
└─────────────┬─────────────┘
              │
              ▼
 Local machine / VPS / Docker
```

## 为什么使用 AgentDock

| 能力 | 说明 |
| --- | --- |
| 统一工具入口 | 通过一个 MCP 服务暴露文件、命令、Git、Skill、任务和浏览器等能力 |
| 本地与远程一致 | 同一套工具模型可运行在 macOS、Linux、Windows 与 Docker 环境 |
| 明确执行边界 | 对路径、输出、权限、超时和敏感信息进行约束与脱敏 |
| 可扩展运行时 | 支持独立 Skill 环境和动态 MCP Server，不需要把所有能力编译进主程序 |
| 可恢复任务 | 支持任务步骤、检查点、阻塞、恢复和最终验证 |
| 面向生产部署 | 提供 Docker、systemd、Windows 与 macOS 安装方式，并持续构建正式镜像 |

## 核心能力

### 工具运行时

- UTF-8 文件读取、搜索、目录遍历和结构化修改
- 有边界的命令执行、长时间会话、PTY 与输出截断
- Git 仓库检查、差异、提交、分支与 GitHub 操作
- 图片读取与浏览器自动化
- macOS 桌面自动化和 Windows / WSL 运行支持

### 扩展系统

- Skill 校验、安装、回滚和独立环境变量管理
- 动态 MCP Server 注册、启停、刷新与隔离配置
- AgentDock 上下文索引，按需发现工具、Skill 和 MCP
- 可选 NexusDock Recall、私密笔记与工作流模板集成

### 任务与可靠性

- 持久化任务状态与阶段检查点
- 阻塞、恢复、最终审查和完成条件
- 命令退出码、工具状态和错误信息分离
- 敏感输出脱敏、私密目录权限与原子文件写入

## 安装

普通用户不需要下载源码、安装 Go 或自行构建镜像。先进入 [安装 AgentDock](https://uvwt.github.io/agentdock-docs/docs/getting-started/install)，按当前环境选择安装方式。

### 平台文档

| 平台 | 文档 |
| --- | --- |
| Docker | [Docker 快速部署](https://uvwt.github.io/agentdock-docs/docs/getting-started/docker) |
| Linux | [自动安装](https://uvwt.github.io/agentdock-docs/docs/getting-started/linux) |
| Linux / VPS | [systemd 部署](https://uvwt.github.io/agentdock-docs/docs/getting-started/vps) |
| macOS | [macOS 安装](https://uvwt.github.io/agentdock-docs/docs/getting-started/macos) |
| Windows | [Windows 原生安装](https://uvwt.github.io/agentdock-docs/docs/getting-started/windows) |

每个页面都提供安装命令、启动检查、MCP 地址和认证方式。浏览器、桌面自动化、WSL、反向代理和数据迁移等内容位于对应进阶文档。

### Docker 快速开始

正式镜像会同步发布到 GitHub Container Registry 和 Docker Hub。Release 中的 Compose 文件默认使用 GHCR 并固定到对应版本，镜像以非 root 用户运行：

```bash
mkdir agentdock && cd agentdock
curl -fL https://github.com/uvwt/agentdock/releases/latest/download/docker-compose.yml \
  -o docker-compose.yml
export AGENTDOCK_AUTH_TOKEN="$(openssl rand -hex 32)"
docker compose pull
docker compose up -d
```

默认 MCP 地址：

```text
http://127.0.0.1:18766/mcp
```

查看运行状态：

```bash
docker compose ps
docker compose logs -f
```

停止服务：

```bash
docker compose down
```

完整配置与客户端接入方式见 [Docker 部署文档](https://uvwt.github.io/agentdock-docs/docs/getting-started/docker)。

### 镜像版本

| 镜像标签 | 用途 |
| --- | --- |
| `latest` / `<version>` | 正式运行镜像，不包含 Go 编译工具链 |
| `dev-latest` / `dev-<version>` | 包含 Go、C、C++ 构建链的开发镜像 |
| `browser-latest` / `browser-<version>` | 包含 Chromium 浏览器环境的自动化镜像 |

正式镜像同步发布到两个 Registry：

```text
ghcr.io/uvwt/agentdock
agentdockio/agentdock
```

## 运行目录

| 路径 | 用途 |
| --- | --- |
| `~/AgentDock` | 相对文件操作的默认工作目录 |
| `~/.agentdock` | AgentDock 状态、配置、会话和扩展数据 |

Docker 部署默认使用 named volume 保存持久化数据，以避免 Linux bind mount 的 UID/GID 冲突。

## 安全模型

AgentDock 会直接操作宿主机或容器内的真实资源，应按照基础设施服务进行部署和授权。

- 仅监听回环地址时可以关闭认证
- 监听非回环地址时必须启用 Bearer Token 或 OAuth
- 对公网提供服务时必须配置 HTTPS 和网络访问控制
- 使用独立系统用户、容器权限和 volume 边界限制可访问资源
- Skill 与动态 MCP 的敏感变量使用独立环境存储，不写入公开配置
- 不授予 AgentDock 超出实际任务所需的文件、命令或网络权限

## 从源码运行

本节面向 AgentDock 贡献者和需要调试运行时的开发者。

```bash
git clone https://github.com/uvwt/agentdock.git
cd agentdock
make check
make run
```

源码模式默认监听 `127.0.0.1:8765`。

## 开发与贡献

提交代码前运行完整检查：

```bash
make check
```

项目使用 GitHub Actions 持续执行测试、静态检查、构建和发布验证。用户文档独立维护在 [`uvwt/agentdock-docs`](https://github.com/uvwt/agentdock-docs)；修改用户可见行为时，应同步更新对应文档。

提交问题或功能建议请使用 [GitHub Issues](https://github.com/uvwt/agentdock/issues)。

## 相关链接

- [在线文档](https://uvwt.github.io/agentdock-docs/)
- [文档源码](https://github.com/uvwt/agentdock-docs)
- [GitHub Releases](https://github.com/uvwt/agentdock/releases)
- [GitHub Container Registry](https://github.com/uvwt/agentdock/pkgs/container/agentdock)
- [Docker Hub](https://hub.docker.com/r/agentdockio/agentdock)

## License

Apache License 2.0. See [LICENSE](./LICENSE).
