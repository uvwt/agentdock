# 日志规范

> AgentDock 日志处理方式。

## 概览

AgentDock 通过 `internal/logx` 使用标准库 `log/slog`。日志以 JSON 写入 stderr，方便 Docker、launchd、systemd 和本地 shell 统一收集。

除非有明确理由，功能包不要直接 import `log/slog`。优先使用 `logx`，让日志级别设置和输出格式保持集中。

## 日志级别

- `debug`：高频诊断细节，必须安全可暴露，通常只在排查时有用。
- `info`：生命周期事件，例如服务启动、选择的 mode/profile、启用的可选子系统、长运行 agent 成功启动。
- `warn`：AgentDock 可继续运行但能力降级，例如可选依赖缺失或环境限制。
- `error`：导致子系统停止、请求中止或需要操作者介入的失败。

## 结构化日志

- 使用稳定的 snake_case key，例如 `tool_profile`、`path_policy`、`memory_enabled`、`nexus_enabled`。
- boolean 和 count 使用原生值，不格式化成字符串。
- 记录 code 或 state 时优先使用包内定义的标识符，不写自由文本。
- 日志消息应简短，面向事件。

## 应记录什么

- 可安全暴露的进程启动配置：workspace 路径、mode、path policy、host、port、profile、启用的可选子系统。
- 服务生命周期和监听地址。
- Nexus agent 生命周期和非 secret 端点元数据。
- 解释可选子系统不可用原因的 warning。
- 带足够上下文、可识别失败层级的 error。

## 不应记录什么

- Authorization header、bearer token、OAuth code、cookie、API key、密码或 secret 环境变量值。
- 完整工具参数载荷、命令输出、request body 或外部 API response，除非已明确脱敏并限制大小。
- 本地私有配置文件内容。
- 可能包含用户私有运维历史的 Memory 内容。

## 常见错误

- 用临时 `fmt.Println` 调试输出替代结构化日志。
- 未脱敏前记录命令或外部服务返回的原始错误。
- 只记录 `ok=true` 这类摘要，却缺少诊断所需的层级上下文。
