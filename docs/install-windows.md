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
- Skill 本身是平台无关文档；包内辅助脚本必须自行检查 Windows 所需的 Python、Node.js、PowerShell 或原生命令依赖。
- macOS `desktop` Skill 不支持 Windows；Windows 桌面自动化应使用独立工具或对应平台的 MCP 能力。

## WSL 命令与文件

Windows AgentDock 不需要额外 Worker 或启动配置。需要 Linux 运行环境时，在现有工具中显式选择：

```json
{
  "runtime": "wsl",
  "wsl_distribution": "Ubuntu"
}
```

`wsl_distribution` 可省略，此时使用系统默认发行版。`exec_command`、`read_file`、`list_dir`、`list_files`、`search_text` 和 `file_edit` 均复用现有工具入口，不新增 WSL 专用工具。

文件工具在 `runtime=wsl` 时使用发行版内的 Python 3 标准库执行原生 Linux 文件操作，因此目标发行版必须安装 `python3`。路径必须是绝对 Linux 路径，例如 `/home/a/project` 或 `/mnt/d/Project`；`D:\Project` 形式的绝对盘符路径会自动转换为 `/mnt/d/Project`。

WSL `file_edit` 的安全边界：

- 仅支持普通 UTF-8 文件，拒绝二进制、目录和设备等特殊文件。
- 拒绝软链接，避免原子替换破坏链接关系。
- 禁止写入 `/proc`、`/sys`、`/dev` 和 `/run`。
- 新内容先写入目标目录的临时文件，完成 `fsync`、保留原权限后再原子替换。
- `patch` 只接受 `*** Begin Patch` 结构化 envelope，且每次调用只允许一个文件操作，以保证写入可恢复。
- `move` 不跨文件系统；目录递归删除不在第一版支持范围内。

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
