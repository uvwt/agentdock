# AgentDock

AgentDock 是本地/远程 Agent 工具运行层，提供文件、命令、Git、动态插件、浏览器自动化、macOS 桌面自动化和 MemoryDock 长期记忆能力。

## 核心能力

- 工作区文件读取、搜索、结构化补丁。
- 受控命令执行、会话管理、输出截断和脱敏。
- Git / GitHub 仓库操作。
- 动态插件 `plugin_*`，无需重新编译核心二进制。
- 可选浏览器自动化 `browser_*`。
- macOS 裸机桌面自动化 `desktop_*`。
- MemoryDock 长期记忆 `memory_*`。

## 快速验证

```bash
go test ./...
go vet ./...
go build ./cmd/agentdock
```

## macOS 裸机更新

```bash
cd ~/agentdock
make check
make install-macos
make restart-macos
make smoke-macos
```

## Docker

```bash
make docker-build
make docker-up
make logs
```

## 文档

- [macOS 裸机 launchd 部署](docs/install-macos-launchd.md)
- [Docker 部署](docs/install-docker.md)
- [VPS systemd 部署](docs/install-vps-systemd.md)
- [macOS desktop 自动化](docs/desktop-automation.md)
- [动态插件](docs/dynamic-plugins.md)
- [MemoryDock](docs/memorydock.md)
- [安全模型](docs/security.md)
- [排障](docs/troubleshooting.md)
- [动态插件 JSON Schema](docs/plugin.schema.json)

## 开发约定

- `main` 是稳定主分支。
- 合并前运行 `make check`。
- Git commit message 使用中文。
- README 只保留入口和原则，细节放入 `docs/`。
- macOS 桌面自动化必须裸机运行；Docker 不提供真实桌面控制。

## License

MIT
