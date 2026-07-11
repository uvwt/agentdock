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


## Windows 本地保护

Windows 原生运行时不把 POSIX `chmod` 当成安全边界：

- AgentDock 状态目录和 0600 语义的原子文件使用受保护 DACL，只授权当前用户、SYSTEM 和本机管理员。
- Env Registry 中 `kind=secret` 的值使用当前用户作用域 DPAPI 加密后落盘；旧明文值只作为迁移输入读取，并在下一次写入时转成 DPAPI 密文。
- 登录自启动脚本保存的 Bearer token同样使用当前用户 DPAPI，任务计划程序以当前登录用户运行。
- Windows Job Object 负责约束命令和 Skill 子进程树；这不是 Codex 等级的 Restricted Token 沙箱。AgentDock Windows Core 仍采用可信 Host 用户权限模型。
