# macOS Desktop 配置与生效

适用于通过 `AgentDock.app` 运行的 macOS Desktop。不要把 Linux 的 systemd 环境文件、Windows runtime root 或 Docker Compose 流程套到这里。

## 配置事实源

当前 macOS Desktop 的运行目录是：

```text
$HOME/Library/Application Support/AgentDock
```

Core 运行环境文件是：

```text
$HOME/Library/Application Support/AgentDock/agentdock.env
```

AgentDock.app 的高级设置控制器会读取这份文件，校验配置后原子写回，并保持私有文件权限。保存成功且 Core 已加载时，App 会重启 Core；如果新配置启动失败，会尝试恢复旧配置并再次重启。

常见可编辑项包括：端口、日志级别、MCP Apps UI、浏览器、已有 CDP、ACP Agent 与 ACP Adapter 参数。

## 推荐修改方式

优先使用 AgentDock.app 的设置/高级设置界面。这样可以复用当前版本的校验、原子写入和失败回滚逻辑。

不要把 `agentdock config update` 当成 macOS Desktop 的通用配置入口：当前非 Windows 实现明确把桌面配置交给原生配置控制器管理。

只有 UI 没有覆盖的高级变量，才考虑手工编辑 `agentdock.env`。手工修改时：

1. 先读取当前文件，不要覆盖未知键；
2. 不回显认证秘密；
3. 保持文件为普通文件且仅当前用户可读写；
4. 修改后重启已经由 AgentDock.app 注册的 Core；
5. 验证 `/healthz` 和目标功能。

如果当前环境存在可用 `agentdock` CLI，Core 重启需要显式 runtime root：

```bash
agentdock service restart --runtime-root "$HOME/Library/Application Support/AgentDock"
```

如果 CLI 不在 PATH，优先由 AgentDock.app 自己保存设置或控制 Core，不要为了重启额外安装第二套 Core。

## 不要做的事

- 不要创建 `~/Library/LaunchAgents/com.uvwt.agentdock*.plist` 作为替代生命周期。当前 macOS Desktop 由 AgentDock.app 的 SMAppService 管理后台 Core/Tunnel。
- 不要把 `~/.agentdock` 当成 macOS Desktop 的 Core 环境文件目录。
- 不要手工覆盖 App Bundle 内的 Core 二进制来“让配置生效”。
- 不要降低 `agentdock.env` 权限以方便其他用户读取。

## 验证

至少检查：

1. AgentDock Core 已恢复运行；
2. 实际监听端口对应的 `/healthz` 返回成功；
3. 本次修改的功能真的变化；
4. 如果 App 报告回滚，读取错误证据并确认旧配置已恢复，而不是继续覆盖文件。
