# 错误处理

> AgentDock 错误处理方式。

## 概览

错误信息应足够具体，便于操作者和客户端判断失败层级：validation、permission、configuration、filesystem、network、runtime 或 protocol。优先返回带上下文的错误，不要只写日志后静默继续。

## 错误类型

- `internal/tools.ToolError` 是 MCP 工具的客户端可见错误类型，包含 `code`、`message`、`category`、`retryable` 和可选结构化 `details`。
- `internal/skillruntime.Error` 是 Skill Runtime 错误类型，包含稳定 code 和 stage，用于 install/run 诊断。
- `internal/commandqueue.HandlerError` 用于 Nexus command adapter，区分可重试和不可重试失败。
- `internal/jsonrpc.Error` 是 MCP server 使用的 JSON-RPC 响应错误结构。

## 错误处理模式

- 输入进入系统的边界处负责校验。
- 内部错误使用 `%w` 包装上下文，让调用方保留因果链。
- 当客户端可以根据错误类别或 details 采取行动时，工具实现返回 `ToolError`。
- permission failure 必须和 validation failure 区分。
- configuration failure 必须和 runtime execution failure 区分。
- 命令输出、环境变量来源的值和工具细节返回客户端前必须脱敏。

## API 错误响应

- MCP 工具错误通过 MCP tool envelope 返回，包含 `isError=true` 和 `structuredContent`。
- JSON-RPC parse 和 params 错误使用 `internal/jsonrpc` 中的 JSON-RPC error code。
- HTTP 鉴权失败返回 `401 unauthorized`；方法不匹配返回 `405 method not allowed`。
- Skill Runtime 结果同时包含 `ok` 和稳定 error code 字段，让调用方不必解析字符串即可判断失败。

## 常见错误

- 直接把可能包含路径、token、命令输出或原始载荷的 `err.Error()` 返回给客户端。
- 把 permission、validation 和 runtime failure 合并成一个泛化错误。
- 打了错误日志却返回 `nil`，导致 smoke test 通过但行为已经坏掉。
- 新增工具时没有覆盖非法参数和 permission/profile 边界测试。
