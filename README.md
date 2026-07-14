# AgentDock

AgentDock 是面向本地与远程 Agent 的工具运行层，提供文件、命令、Git、Skill、动态 MCP、浏览器自动化、可恢复任务和 NexusDock Recall 集成。

[在线文档](https://uvwt.github.io/agentdock-docs/) · [文档源码](https://github.com/uvwt/agentdock-docs)

## 核心能力

- 文件读取、搜索和结构化修改。
- 有边界的命令执行、长时间会话和输出脱敏。
- Git / GitHub 仓库操作。
- 纯文档 Skill 与独立环境管理。
- 动态 MCP Server 注册、发现和调用。
- 可恢复任务、Workflow 模板与阶段验证。
- 可选浏览器自动化和 macOS 桌面自动化。
- 可选 NexusDock Recall 长期知识召回。

## 安装

普通用户不需要下载源码、安装 Go 或自己构建镜像。先进入 [安装 AgentDock](https://uvwt.github.io/agentdock-docs/docs/getting-started/install)，按当前环境选择：

- [macOS 安装](https://uvwt.github.io/agentdock-docs/docs/getting-started/macos)
- [Windows 安装](https://uvwt.github.io/agentdock-docs/docs/getting-started/windows)
- [Linux 安装](https://uvwt.github.io/agentdock-docs/docs/getting-started/linux)
- [Docker 安装](https://uvwt.github.io/agentdock-docs/docs/getting-started/docker)

每个页面都给出安装命令、启动检查、MCP 地址和认证方式。需要浏览器、桌面自动化、systemd、WSL、反向代理或数据迁移时，再进入对应进阶文档。

## 开发者从源码运行

普通安装不需要 Go 或源码。本节只面向参与开发和调试 AgentDock 的贡献者。使用 `go.mod` 声明的 Go 版本：

```bash
git clone https://github.com/uvwt/agentdock.git
cd agentdock
make check
make run
```

默认监听 `127.0.0.1:8765`。相对文件路径从 `~/AgentDock` 解析，内部状态保存在 `~/.agentdock`。

## 开发

提交前运行：

```bash
make check
```

公开文档统一维护在 [`uvwt/agentdock-docs`](https://github.com/uvwt/agentdock-docs)。修改用户可见行为时，应在同一任务中同步更新文档仓库。

## 安全

仅回环地址可以无认证运行。监听非回环地址时必须配置 Bearer Token 或 OAuth，并配合 HTTPS、运行用户权限、Docker volume 或 systemd 隔离控制可访问范围。

## License

MIT
