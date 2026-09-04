# Windows Desktop 配置与生效

适用于官方 Windows Desktop / Setup 安装。不要把 macOS `agentdock.env`、Linux EnvironmentFile 或 Docker Compose 当成 Windows Desktop 的配置事实源。

## 运行目录与配置事实源

官方安装器默认把二进制放在：

```text
%LOCALAPPDATA%\AgentDock\bin
```

因此默认 runtime root 是：

```text
%LOCALAPPDATA%\AgentDock
```

安装器允许改变安装目录，所以操作前应优先读取当前 `runtime.json`、控制面板显示的配置目录，或实际启动参数中的 `--runtime-root`，不要只依赖默认值。

Windows Desktop 的运行配置由多部分组成：

- `control-panel-settings.json`：端口、日志、MCP Apps UI、浏览器、ACP 等普通设置；
- `runtime.json`：安装位置、Core、Tray、Tunnel 与启动方式等运行清单；
- `auth-token.dpapi`、`oauth-password.dpapi`、`oauth-token-secret.dpapi`、`cloudflared-token.dpapi`：受当前 Windows 用户保护的秘密；
- `server-url.txt`、Tunnel 状态文件等：公网/OAuth/Tunnel 运行状态。

Core 启动时会读取这些状态并生成实际进程环境。

## 推荐修改方式

优先使用 AgentDock Windows 控制面板。保存设置时，控制面板会调用当前 Core 的结构化配置入口；该入口会校验新设置、保存配置、重启 Core/Tunnel，并在失败时恢复原文件后尝试恢复旧运行状态。

如果必须通过 CLI 修改控制面板覆盖的普通设置，应使用当前安装目录里的 `agentdock.exe config update --runtime-root <实际目录> ...`，不要手工拼写另一份 JSON 并假定所有字段都会被读取。

## 手工修改边界

- 不要把 Bearer Token、OAuth 密码、OAuth 签名密钥或 Tunnel Token 写进 `control-panel-settings.json`。
- 不要手工解密、复制或跨用户迁移 DPAPI 文件；它们绑定 Windows 用户保护上下文。
- 不要只修改 `runtime.json` 来改变普通设置；它主要描述运行时布局与启动状态。
- 如果手工改了 `control-panel-settings.json`，旧 Core 不会自动重新读取，仍需重启实际 Core。
- 提权模式可能由 Scheduled Task 启动 Core；标准模式可能由普通后台进程启动。不要用“杀掉一个同名进程”代替官方 service/control-panel 操作。

## 生效与验证

优先通过控制面板执行保存/重启。需要 CLI 时，使用当前实际 runtime root，例如默认安装可表示为：

```powershell
agentdock service restart --runtime-root "$env:LOCALAPPDATA\AgentDock"
agentdock service status --runtime-root "$env:LOCALAPPDATA\AgentDock"
```

如果安装目录不是默认值，把路径替换成 `runtime.json` 所在目录。

验证至少包括：

1. `service status` 或控制面板显示 Core running/healthy；
2. 当前端口的 `/healthz` 成功；
3. 本次设置对应的功能真的变化；
4. 如果配置更新返回回滚错误，检查当前文件和 Core 状态是否已恢复，不要继续覆盖 DPAPI 或 runtime 文件。
