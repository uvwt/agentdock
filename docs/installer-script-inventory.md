# Installer 脚本职责盘点

本页记录 Installer 最终收口后的稳定边界。目标不是把所有平台操作强行改成 Go，而是让产品状态机只有一个权威实现：安装事务、generation、manifest、Skill、服务/Tunnel 生命周期归 Go Installer Engine / native CLI；脚本只保留真正的 bootstrap 与 OS bridge。

## Release 公开面

新 Release 只有两个公开脚本入口：

- `install.sh`：Linux/macOS 统一 bootstrap，直接下载对应 Release payload、校验后调用 Go Installer Engine；`--uninstall` 也走同一入口。
- `install.ps1`：Windows 公开 bootstrap / Setup 入口。

不再发布或保留 `install-linux-platform.sh`、`install-macos-platform.sh`、`uninstall-linux.sh`、`uninstall-macos.sh`。`uninstall-windows.ps1` 仍随 Windows Setup 内部使用，但不是公开 Release API。历史 Release 的既有资产不受影响。

Release Catalog 由 `tools/release` 约束；测试禁止重新把 `runtime-adapter` 类型脚本加入公开 Catalog。

## 当前脚本边界

| 文件 | 当前规模 | 契约 | 保留理由 |
|---|---:|---|---|
| `scripts/install/install.sh` | 634 | 公开 | Unix bootstrap：平台/架构识别、下载校验、必要权限/service-user 前置，再调用 Engine |
| `scripts/install/install.ps1` | 2257 | 公开 | Windows bootstrap/外层 adapter：UAC、DPAPI、HKCU Run、Setup Result、Task 管理与 rollback |
| `scripts/install/uninstall-windows.ps1` | 354 | 内部 | Windows 自删除、Task/Registry 清理、detached Engine commit；不作为 Release API |
| `scripts/install/launch-windows-process.ps1` | 223 | 内部 | Inno RedirectionGuard 外启动当前用户 session 的临时 Scheduled Task broker |
| `scripts/install/probe-protected-text.ps1` | 35 | 内部 | 只读 DPAPI 可用性探针，不返回凭据明文 |

`manage-windows.ps1` 已删除。它原来唯一仍有产品调用的 `task-run-session` 已迁到原生：

`agentdock service task-start --task-name <name> [--expected-user-sid <sid>]`

实现使用 Windows Task Scheduler COM `RunEx(TASK_RUN_USE_SESSION_ID)` 与现有 interactive-session 选择逻辑。其他历史 manager Action 没有产品调用方，不再保留转发层。

`install.ps1` 中仍出现一次旧 `installer\manage-windows.ps1` 路径，仅用于升级清理：安装开始时纳入现有 runtime backup，成功后删除，失败时 `Restore-FileState` 恢复给旧版本。它不是可执行兼容入口，也不会进入当前 payload。

## Go Installer Engine 的单一权威

`internal/installer/` 负责：

- install / repair / uninstall / commit / abandon 事务与中断恢复；
- Windows generation publish / attach / same-version repair 与 active-version 两阶段 pointer；
- Windows stable CUI/GUI shim 与 icon 的实际发布；
- `runtime.json` 与 `desktop-version.txt` 投影；
- legacy v0.8.x 布局通过 `install prepare-windows-legacy` 建立 known-good source generation；
- rollback journal；
- Core Skills bootstrap；
- systemd / OpenRC / launchd 与 Windows native service/Tunnel 生命周期；
- inspect 权威状态投影和 known-good 判断。

Windows Release payload 必须支持 `install --engine-ready`。不再存在 non-engine-ready PowerShell fallback：脚本不会自己 staging generation、写 manifest 或 bootstrap Skills。

stable shim/icon 的职责也已去重：PowerShell 在调用 Engine 前只备份旧 stable 文件，Engine 是唯一写入者；如果后续 Task/Registry adapter 失败，PowerShell 用旧备份恢复外层状态。

## 为什么 Windows 仍保留 PowerShell

`install.ps1` 剩余代码属于五类边界：

1. **bootstrap**：Release/离线 archive 获取、SHA-256、架构与 payload preflight；必须在停止旧进程前完成。
2. **Windows 凭据**：DPAPI auth/OAuth/Tunnel token 的读取、生成、不可读备份与复用。
3. **外部 OS 状态**：HKCU Run、管理员 Task 的 UAC 创建/恢复、当前用户/interactive session 判定。
4. **两阶段 adapter**：调用 Engine `--defer-commit`，外部状态成功后 commit；失败时先恢复 Task/Registry/文件与运行态，再 abandon/标记 rollback failure。
5. **Setup 契约**：UTF-16 Result INI、Inno 参数、RedirectionGuard 下的 runtime broker。

这些不是另一套产品状态机。generation、manifest、Skill 和 native service/Tunnel 决策不得重新放回 PowerShell。

`uninstall-windows.ps1` 同样只承担 Engine 无法在删除自身后继续完成的 Windows 外层动作：删除管理员 Task、HKCU Run、停止占用目标文件的进程、删除安装文件，再用 transaction-id scoped detached Engine helper 最终 commit。任何关键清理失败都不得返回成功。

## Unix 最终状态

`install.sh` 是 Linux/macOS 唯一脚本入口。它不会下载或 dispatch platform installer/uninstaller 脚本。测试 fake Release 只提供 payload tarball 和必要依赖，以证明不存在隐藏依赖。

macOS Release smoke 直接使用 `install.sh --uninstall`；Linux VPS E2E 同样走统一入口。平台服务模板、Quick Tunnel 状态机与卸载事务均在 Go 实现。

## 不允许回归

- Release 不得重新发布 Unix platform installer/uninstaller 或 Windows internal uninstall adapter。
- 不得重新引入 `manage-windows.ps1`、`task-run-session` 产品调用或 PowerShell Tunnel/service 状态机。
- Windows payload 必须 Engine-ready；不得恢复 non-engine-ready generation/manifest/Skill fallback。
- active-version、runtime manifest、desktop version 与 generation publish 必须由 Engine 单一拥有。
- legacy v0.8.x → generation 必须保留 known-good fallback，不能用“去兼容”破坏公开升级路径。
- uninstall 必须满足 defer-commit、可重入 helper、helper 丢失响亮失败和 external rollback failure 阻塞下一次安装。
- Windows Task ownership 不得误伤其他安装；测试必须使用隔离 root/启动标识并保护生产 Task/8765。
- 新增或删除脚本必须同步 `scripts/governance/inventory.yaml`，治理测试按当前工作树真实存在的 tracked 文件校验。
