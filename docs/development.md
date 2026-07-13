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

## Skill 开发规范

Skill 是给模型读取的工作方法，不是可执行工具。第一方 Skill 必须遵循“可移植核心 + 可选宿主适配”分层：核心流程只使用包内相对路径、当前进程环境和通用协议；AgentDock 专属的 `skill://`、`exec_command skill=<name>` 和 `skill_package` 只负责发现、宿主绑定与生命周期。

完整设计、执行语义和禁止项见 [Skill 设计与运行模型](skills.md)。开发时至少满足：

- 包根目录包含合法 `SKILL.md`，Frontmatter 只声明 `name`、`description`、`version`；
- 不支持额外运行清单、统一执行入口或动作 Schema；
- 包内脚本从 Skill 根目录使用相对路径运行，只从当前进程环境读取配置和凭据；
- 不手工读取 Skill state、拼接已安装版本目录或 `source` AgentDock 私有环境文件；
- 安装包不包含 `.env`、符号链接、缓存、编译产物或设备私有状态；
- 修改正文、引用或脚本后递增语义化版本；同名同版本内容不可变；
- 先运行 `skill-authoring lint` 检查创作质量与可移植性，再运行 `skill_package validate` 检查包能否合法安装；
- 安装后通过 `agentdock_context`、`skill://` 和 `exec_command skill=<name>` 验证当前激活版本。

`skill-authoring lint` 与 `skill_package validate` 不能互相替代。前者判断第一方 Skill 是否把宿主耦合写进核心契约，后者只负责包结构、安全边界和安装合法性。

平台相关辅助脚本应自行声明和检查依赖。公共 Go 文件不能直接引用 Unix `syscall` 或 Windows API；AgentDock 自身命令和交互终端的进程树统一通过 `internal/processcontrol` 管理。
