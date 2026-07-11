# Windows 原生安装

AgentDock Windows Core 支持 Windows 11 x64 和 ARM64。它原生使用 PowerShell、Windows Job Object、ConPTY、受保护 DACL 和当前用户 DPAPI，不要求 WSL2。

## 安装

在 PowerShell 中运行：

```powershell
powershell -ExecutionPolicy Bypass -File scripts/install-windows.ps1
```

远程安装：

```powershell
$script = Join-Path $env:TEMP 'install-agentdock.ps1'
Invoke-WebRequest https://raw.githubusercontent.com/uvwt/agentdock/main/scripts/install-windows.ps1 -OutFile $script
powershell -ExecutionPolicy Bypass -File $script
```

安装脚本会：

1. 自动识别 x64 或 ARM64。
2. 下载对应 Release ZIP 和 SHA-256 校验文件。
3. 安装到 `%LOCALAPPDATA%\AgentDock\bin`。
4. 把安装目录加入当前用户 PATH。
5. 创建 `%USERPROFILE%\.agentdock` 和 `%USERPROFILE%\AgentDock`，并收紧 DACL。

默认只安装二进制，不创建后台任务。需要登录后自动启动：

```powershell
powershell -ExecutionPolicy Bypass -File scripts/install-windows.ps1 -RegisterStartup -Port 8765
```

此模式会生成 Bearer token，只显示一次；磁盘中只保存当前用户 DPAPI 密文。任务计划程序以当前登录用户运行 AgentDock，并验证 `/healthz`。

## 命令与 Skill

- `exec_command` 优先使用 PowerShell 7，找不到时使用 Windows PowerShell，最终回退到 `cmd.exe`。
- `tty=true` 使用 Windows ConPTY。
- 声明 Windows 兼容的 Skill 必须在 `agentdock.yaml` 明确设置 `spec.runtime`：`binary`、`python`、`node` 或 `powershell`。
- macOS `desktop` Skill 不支持 Windows。Windows 桌面自动化属于后续独立 Skill。

## 浏览器

Windows browser runner 可发现系统 Chrome 和 Edge，包括 `Program Files`、`Program Files (x86)` 和当前用户 `LOCALAPPDATA` 安装位置。浏览器工具仍需 Node.js 和 `playwright-core` runner 依赖。

## 卸载

保留 AgentDock 状态：

```powershell
powershell -ExecutionPolicy Bypass -File scripts/uninstall-windows.ps1
```

同时删除 `%USERPROFILE%\.agentdock` 和 `%USERPROFILE%\AgentDock`：

```powershell
powershell -ExecutionPolicy Bypass -File scripts/uninstall-windows.ps1 -PurgeState
```
