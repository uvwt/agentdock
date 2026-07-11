# 开发与质量门禁

本文档定义 AgentDock 默认本地开发和产品化检查方式。

## 质量门禁

完成改动前运行完整门禁：

```bash
make check
```

`make check` 会格式化、测试、vet 并构建项目：

```bash
gofmt -w ./cmd ./internal
go test ./...
go vet ./...
go build -trimpath -o ./bin/agentdock ./cmd/agentdock
```

局部快速迭代时，可以先运行包级测试，最后用 `make check` 收尾。

## 本地产物

macOS 裸机部署时，仓库根目录可能包含本地运行产物：

- `agentdock`
- `agentdock.new`
- `agentdock.prev.*`
- `agentdock.bak*`
- `agentdock.killed*`
- `bin/`
- `coverage.out`

这些文件会被 git 忽略。根目录的 `agentdock` 二进制可能是 Mac mini 上由 launchd 管理的当前宿主机二进制，因此普通仓库清理时不要删除它。

清理历史本地产物时使用：

```bash
make clean-local-artifacts
```

该目标只删除被 `.gitignore` 明确覆盖的历史产物和构建目录，不删除当前运行用的根目录 `agentdock` 二进制。

发布产物应在 git 跟踪的源码之外生成，例如放在 `dist/` 或由发布流水线处理。

## 文档边界

- `README.md` 保持简洁：产品摘要、快速验证、常用部署入口和链接。
- 运维 runbook 放在 `docs/`。
- AI/开发者代码规则放在 `.trellis/spec/`。
- 不在文档中记录真实 token、cookie、OAuth code、私有端点或本地 secret 值。

## 改动要求

- 改动范围限制在被修改的包和行为内。
- 新增抽象前优先复用已有 helper 和包模式。
- 保持高风险工具的权限门禁、路径策略、认证和日志脱敏边界。
- 修改工具描述、Schema、路径策略、权限、命令执行、HTTP 认证或桌面/浏览器自动化时，必须同步更新测试。

## Skill 包规范

Skill 是给模型读取的工作方法，不是可执行工具。包根目录必须包含 `SKILL.md`，Frontmatter 必须声明：

```yaml
---
name: example-skill
description: 何时使用以及解决什么问题
version: 1.0.0
---
```

约束如下：

- 不支持额外的运行清单、统一执行入口或动作 Schema。
- 安装包不能包含 `.env`、符号链接、缓存或编译产物。
- 引用资料放在 `references/`，可复用脚本放在 `scripts/` 或包根目录。
- 模型先通过 `agentdock_context` 选择 Skill，再用 `read_file skill://<name>/SKILL.md` 读取正文。
- 实际动作使用 `exec_command`、文件工具、浏览器工具或 MCP 工具。
- 包内辅助脚本如需多个动作，从 stdin JSON 的 `skill_action` 字段选择；业务参数保留在同一个 JSON 对象中。
- 凭据和设备私有配置放在 `~/.agentdock/skill-data/<name>/.env`，权限必须为 `0600`，不得提交到仓库或随包安装。
- 修改 Skill 正文、引用或脚本后必须递增版本，校验并安装新版本；同名同版本内容不可变。

平台相关辅助脚本应自行声明和检查依赖。公共 Go 文件不能直接引用 Unix `syscall` 或 Windows API；AgentDock 自身命令和交互终端的进程树统一通过 `internal/processcontrol` 管理。
