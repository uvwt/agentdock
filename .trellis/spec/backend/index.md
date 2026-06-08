# 后端开发规范

> AgentDock 后端代码修改、审查和 AI 辅助开发约定。

## 概览

AgentDock 是一个 Go 服务，用于向本地和远程 Agent 暴露 MCP 工具。后端是单一 Go module，可执行入口位于 `cmd/agentdock`，具体实现包位于 `internal/`。

这些规范记录当前项目的真实约定。新增抽象前先遵循现有包边界；面向产品的行为必须明确、可测试，并默认安全。

## 规范索引

| 规范 | 说明 | 状态 |
|-------|-------------|--------|
| [目录结构](./directory-structure.md) | 模块组织和文件布局 | 生效 |
| [存储规范](./database-guidelines.md) | 持久化状态和存储策略 | 生效 |
| [错误处理](./error-handling.md) | 错误传播、工具错误和 API 响应 | 生效 |
| [质量规范](./quality-guidelines.md) | 代码标准、质量门禁和禁止模式 | 生效 |
| [日志规范](./logging-guidelines.md) | 结构化日志、日志级别和敏感信息规则 | 生效 |
| [Skill Runtime 规范](./skill-runtime-guidelines.md) | 原生 Skill manifest、env 声明和运行时注入合同 | 生效 |

## 开发前检查

- 阅读本次改动涉及包对应的规范。
- 新增 helper 或包模式前，先搜索是否已有同类实现。
- 新增面向用户的能力时，必须放在合适的 profile、权限、运行时开关或 Skill Runtime 边界后。
- 不打印、不持久化 secret、token、cookie、OAuth code 或原始工具载荷。
- 完成前运行 `make check`；如果无法通过，记录具体失败命令和原因。

## 质量检查

- 运行 `make check`。
- 修改工具、路径、权限、Skill Runtime、env registry、HTTP auth、命令执行或自动化能力时，重新阅读 [质量规范](./quality-guidelines.md)。
- 修改返回错误、诊断信息、日志或外部服务处理时，重新阅读 [错误处理](./error-handling.md) 和 [日志规范](./logging-guidelines.md)。
- 确认本地构建/运行产物仍被忽略，没有进入提交。
- 确认 README、`docs/`、脚本和 Makefile 描述的是同一条验证路径。

## 产品化原则

- README 只作为产品入口；详细运维说明和工程规则放在 `docs/` 与 `.trellis/spec/`。
- 本地运行产物、生成的二进制、覆盖率文件和回滚副本不进入 git。
- 新增具体应用自动化能力应打包为原生 Skill Runtime 能力，不新增旧动态 plugin 路径。
- 新增或修改工具时，工具描述、input schema、output schema、runtime dispatch 和测试必须同步更新。
