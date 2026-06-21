# 安全模型

AgentDock 通过工具 profile、路径策略、命令沙箱、认证和日志脱敏降低风险。

## 认证

- `/healthz` 用于本地健康检查。
- `/mcp` 在配置认证时必须拒绝无授权请求。
- 日志只记录请求元数据，不记录 Authorization header、OAuth code、工具参数正文或 secret 值。

## Profile

`read-only` profile 只能暴露观察类工具。高风险工具如 `desktop_click`、`desktop_type`、`exec_command`、写入类 RecallDock 工具不能出现在 read-only profile 中。

## Host 模式

macOS 裸机桌面自动化必须使用 host 模式；这是可信本机部署，不应作为公网无保护入口暴露。

## 验证建议

```bash
go test ./internal/mcp ./internal/tools ./internal/httpx
go vet ./...
curl -i http://127.0.0.1:18766/mcp
```

无授权访问 `/mcp` 应返回 unauthorized。
