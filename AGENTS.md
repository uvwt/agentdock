# AgentDock Codex / AI Review Instructions

本仓库是 AgentDock：一个 Go 编写的本地/远程 Agent 工具运行层，支持命令、文件、Git、原生 Skill、浏览器自动化、实验性 macOS 桌面能力和 RecallDock 长期召回。

## 默认工作方式

- 默认使用中文给出审查意见、问题说明和提交摘要。
- 不要只给泛泛建议；指出具体文件、具体风险和可执行修复方式。
- 修改前先看现有实现、测试和运行方式，避免引入与现有架构冲突的方案。
- 不要提交或打印密钥、Token、Cookie、私有域名凭据和本地敏感配置值。
- 不要把临时排障过程、未验证猜测或一次性日志写入长期文档。

## Go 项目检查命令

优先使用：

```bash
go test ./...
go vet ./...
go build ./cmd/agentdock
```

格式化使用：

```bash
gofmt -w ./cmd ./internal
```

仓库已有 Makefile 时也可以使用：

```bash
make check
```

## 审查重点

重点检查：

- 工具权限边界是否清晰，是否错误暴露高风险动作。
- 命令执行、文件写入、Git 操作、原生 Skill、桌面自动化相关代码是否存在越权、路径穿越、命令注入、敏感信息泄露问题。
- RecallDock / `recall_*` 调用是否遵循：重要任务先 bootstrap，长期召回只写稳定事实，写入前先查已有内容。
- 错误处理是否可诊断，是否明确区分配置、权限、网络、构建、运行时和插件逻辑问题。
- 修改后是否包含必要测试，或至少说明无法自动测试的原因。
- README / runbook 是否只记录配置原则，不记录真实私密值。

## 工具输出状态约定

- MCP 工具调用是否失败只由协议层 `isError` 表达，模型可见的 `structuredContent` 不再重复返回通用 `ok` 或 `tool_ok`。
- `exec_command`、`session_observe`、`session_act` 等命令结果在命令完成后使用 `command_ok`、`exit_code` 和可选的 `command_error`；命令仍在运行时不返回 `command_ok`。
- 浏览器操作使用 `browser_ok`、`browser_error`，其他工具使用 `valid`、`changed`、`configured`、`written` 等领域字段，不复用含义模糊的通用成功标记。
- 业务结果为失败不等于 MCP 工具调用失败。例如校验得到 `valid: false` 时仍可正常返回；只有参数、权限、资源、网络或内部执行错误才进入 MCP `isError: true`。
- WSL 子进程、HTTP 健康检查等内部协议可以保留自己的状态字段，但不得把内部通用 `ok` 泄漏到 MCP 工具结果中。

## 不建议的审查意见

- 不要只因为个人风格要求大规模重构。
- 不要建议引入笨重依赖来解决小问题。
- 具体工作方法通过纯文档 Skill 提供，真实能力通过内置工具、命令或 MCP 提供，不要把业务能力强绑定进核心代码。
