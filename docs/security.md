# 安全模型

AgentDock 通过路径策略、认证、运行用户隔离和日志脱敏降低风险。

## 认证

- `/healthz` 用于本地健康检查。
- `/mcp` 在配置认证时会拒绝无授权请求。
- HTTP 监听地址只要不是回环地址，就必须配置 `AGENTDOCK_AUTH_TOKEN` 或启用 OAuth；无认证只允许本机回环和 stdio。
- OAuth 注册与密码限流默认只使用 TCP 直接对端地址。只有显式配置 `AGENTDOCK_TRUSTED_PROXY_CIDRS` 后才解析可信代理提供的 `X-Forwarded-For`。
- 日志只记录请求元数据，不记录 Authorization header、OAuth code、工具参数正文或 secret 值。

## 工具边界

AgentDock 不提供工具级权限治理，也不通过工具 profile 裁剪工具集。安全边界来自监听地址、认证、当前 OS 用户权限、Docker volume、systemd 用户和网络策略。

命令会话最多同时运行 32 个，完成结果最多保留 128 个；进程退出时会终止仍在运行的命令树。HTML、SVG、XML 等主动 Artifact 始终作为附件下载，所有公开 Artifact 响应都带有 sandbox CSP 和 `nosniff`。

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
- Skill 包不得包含凭据；环境变量放在当前用户的 `~/.agentdock/env/skill/<name>.env`，其他状态、缓存和会话数据仍放在 `~/.agentdock/skill-data/<name>/`，由操作系统用户权限和部署边界保护。Windows 上保存长期敏感值时应优先使用凭据管理器或当前用户作用域 DPAPI。
- 登录自启动脚本保存的 Bearer token同样使用当前用户 DPAPI，任务计划程序以当前登录用户运行。
- Windows Job Object 负责约束 AgentDock 启动的命令进程树；这不是 Codex 等级的 Restricted Token 沙箱。AgentDock Windows Core 仍采用可信 Host 用户权限模型。
