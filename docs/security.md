# 安全模型

AgentDock 通过路径策略、认证、运行用户隔离和日志脱敏降低风险。

## 认证

- `/healthz` 用于本地健康检查。
- `/mcp` 在配置认证时会拒绝无授权请求。
- 日志只记录请求元数据，不记录 Authorization header、OAuth code、工具参数正文或 secret 值。

## 工具边界

AgentDock 不提供工具级权限治理，也不通过工具 profile 裁剪工具集。安全边界来自监听地址、认证、当前 OS 用户权限、Docker volume、systemd 用户和网络策略。

## Host 模式

macOS 裸机桌面自动化需要本机运行；这是可信本机高权限部署，不建议作为公网无保护入口暴露。

## 验证建议

```bash
go test ./internal/mcp ./internal/tools ./internal/httpx
go vet ./...
curl -i http://127.0.0.1:18766/mcp
```

无授权访问 `/mcp` 应返回 unauthorized。
