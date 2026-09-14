# Installer 脚本职责盘点（第二阶段收口基线）

基线：`fix/installer-engine-mini-20260913` @ `449adba`，对照 `origin/main` @ `f1dde3a`。

本文是 Installer Engine 第二阶段收口（脚本瘦身）的工作地图。它记录：

1. 各脚本的职责分类与真实调用图；
2. Go Installer Engine 已经拥有的决策；
3. 仍留在脚本里的产品业务决策（迁移目标）；
4. `manage-windows.ps1` 逐 Action 处置与删除条件；
5. 不允许回归的语义清单。

行号以基线 `449adba` 为准。行号会随迁移漂移，函数名是稳定锚点。

## 1. 脚本规模与分类（449adba）

| 脚本 | 行数 | 治理分类 | 说明 |
|---|---|---|---|
| `scripts/install/install.ps1` | 2431 | runtime-adapter（公开契约） | Windows Setup / 自更新安装入口 |
| `scripts/install/manage-windows.ps1` | 1017 | legacy（公开契约） | 旧 launcher/runtime 兼容垫片，action 集被治理测试冻结 |
| `scripts/install/uninstall-windows.ps1` | 336 | runtime-adapter（公开契约） | Windows 公开卸载入口 |
| `scripts/install/launch-windows-process.ps1` | 220 | runtime-adapter（公开契约） | Setup 进程树到用户 session 的薄 broker，保留 |
| `scripts/install/install.sh` | 685 | bootstrap（公开契约） | Unix 公开安装入口 |
| `scripts/install/install-linux-platform.sh` | 1702 | runtime-adapter（公开契约） | Linux 平台安装器（新路径已委托 `agentdock install`） |
| `scripts/install/install-macos-platform.sh` | 1701 | runtime-adapter（公开契约） | macOS 平台安装器（新路径已委托 `agentdock install`） |
| `scripts/install/uninstall-linux.sh` | 401 | runtime-adapter（公开契约） | Linux 公开卸载入口 |
| `scripts/install/uninstall-macos.sh` | 125 | runtime-adapter（公开契约） | macOS 公开卸载入口 |

治理基线见 `scripts/governance/inventory.yaml` 与 `scripts/test/script_governance_test.go`：

- `TestManageWindowsActionsStayFrozen` 冻结 `manage-windows.ps1` 的 ValidateSet action 集；
- `TestNewCodeMustNotAddManageWindowsCallers` 禁止新增调用方；
- 超过 300 行的脚本必须声明 `legacy_oversize` 或 `oversize_justification`。

## 2. 调用图（真实调用方，449adba）

### install.ps1
- `packaging/windows/AgentDock.iss:69,75`：随 Setup 打包（`{app}\installer`）。
- `packaging/windows/includes/code.iss:461-502`：Inno `PrepareToInstall` 以
  `-Version -OfflineArchive -InstallDir -TunnelMode -InstallChannel setup -CorePrivilegeMode -ResultFile` 等参数调用；`:513-542` 解析结果 INI。
- `.github/workflows/release.yml:426`：发布到 `dist/install.ps1`（公开下载入口，release smoke 做字节对比）。
- `.github/workflows/windows-installer.yml:70-74,238,249`：CI E2E。
- `scripts/test/test-install-windows-e2e.ps1:205-222`、`test-windows-quick-tunnel-lifecycle.ps1:155-170`、`test-windows-setup-activation-deferred.ps1:71-88`：测试调用。

### manage-windows.ps1
- 随 release zip 发布（`.github/workflows/release.yml:311-312`）与 Setup payload 打包（`AgentDock.iss:71`、`code.iss:441`）。
- Go 侧发布：`internal/installer/apply.go:516-518`（→ `{install-root}\installer\`）、`internal/selfupdate/desktop_apply_windows.go:24,183`（自更新 staging）。**因此该文件是运行时 payload 的一部分，不能删除，只能逐 Action 退役。**
- 脚本调用点：`install.ps1:751-755`（`task-run-session`）、`launch-windows-process.ps1:176-180`（`task-run-session`）、生成的 start-cloudflared.ps1 兼容 launcher（`manage-windows.ps1:783` 写入 `launch-tunnel`）。
- 测试调用：`test-install-windows-e2e.ps1:150,179-181`（`restart`）、`test-windows-task-scheduler.ps1:12`（`task-run-session`）、`test-windows-setup-e2e.ps1:163`。

### uninstall-windows.ps1
- `packaging/windows/includes/code.iss:582-617`：Inno 卸载步骤调用（`-KeepInstallDir` / `-PurgeState`）。
- `.github/workflows/release.yml:427`：dist 发布。
- `scripts/test/test-windows-quick-tunnel-lifecycle.ps1:358-362`。

### install.sh / 平台脚本 / 卸载脚本
- `install.sh`：`Makefile:50`、release 流程（`release.yml:421,602-615,729-743`）、`test-install-entry.sh`。
- `install-linux-platform.sh`：由 `install.sh:608,641,650,659,674,680` 选择并下载执行；`install-macos-platform.sh` 由 `install.sh:612,642` 以 zsh 执行。
- `uninstall-linux.sh`：由 `install.sh:147-148,630-637` 下载/转发。
- `uninstall-macos.sh`：`Makefile:58-59`、`test-install-macos.sh:843-905`。

## 3. Go Installer Engine 已拥有的决策（不再迁移，禁止在脚本重复实现）

`internal/installer/`（非测试约 5000 行）：

| 能力 | 位置 |
|---|---|
| install/repair/uninstall/commit/abandon 事务与中断恢复 | `engine.go` |
| Windows generation 发布 / attach / active-version.json 两阶段 pointer | `apply.go:570-841` |
| runtime.json manifest 生成（engine-ready 路径） | `apply.go:443-557` |
| legacy → generation 一次性迁移 | `legacy_windows.go`（CLI：`install prepare-windows-legacy`） |
| rollback journal 与有序恢复 | `journal.go` |
| 服务/tunnel 生命周期（systemd/OpenRC/launchd/Windows native CLI） | `service.go` |
| uninstall defer-commit + detach-engine 辅助 | `engine.go:611-630`、`cmd/agentdock/command_install_windows.go:43-92` |
| inspect 权威投影 + AssertCommitted | `inspect.go` |
| known-good 判定 / Windows pointer 对账 | `engine.go:95-137,799-809` |

脚本侧引擎调用合同（现状）：`install --engine-ready` 探针、`install --defer-commit ...`、`install commit/abandon`、`install inspect`、`uninstall --defer-commit`、`install detach-engine`、`install prepare-windows-legacy`。

## 4. 仍留在脚本里的产品业务决策（迁移目标）

### install.ps1（Commit 2 / 3 靶子）

| 位置（函数/代码段） | 决策 | 迁移方向 | 状态 |
|---|---|---|---|
| 主流程 1460-1508 | 手工解析 `active-version.json`，自行判断 pointer state 是否 committed、是否要先跑 update-engine 恢复 | 扩展 `install inspect`（新增 transaction/action/generation layout/pointer state 字段），脚本只消费结构化结果；触发恢复的"运行 stable binary"保留在脚本（那是执行动作，不是状态判断） | ✅ Commit 2：`inspect` 新增 `pointer_state` / `pointer_active_version` / `pointer_fallback_version` / `pending_update_transaction` / `transaction_id` / `action`，脚本改为消费 inspect |
| 主流程 1537-1574 | 判定"当前是不是 legacy、是否需要 bootstrap source generation"，并自己读 legacy 版本 | `inspect` 返回 layout=legacy；legacy 版本探测并入引擎（`prepare-windows-legacy` 无 `--legacy-version` 时自行读取），脚本只转发 | ✅ Commit 2：legacy 门改用 `pointer_state -eq 'missing'`；legacy 版本探测保留在脚本（对已安装 binary 的 payload 探测，属 OS bridge 邻接动作） |
| 主流程 1650-1653 | `$engineOwnsTargetGeneration = engineReady && (fresh layout ‖ version differs)` —— generation 归属决策 | 引擎返回结构化判定（inspect 或 install Result 字段），脚本不再组合布尔决策 | ✅ Commit 2：归属决策全部在 `stageWindowsPayload` 内部；脚本只剩 `-not $engineReady` 能力判断 |
| 主流程 1654-1687 | PowerShell 侧 generation staging（same-version repair `.repair-backup` 移动 + crashed-bootstrap 清理） | 收口进 `apply.go`（引擎接管 same-version repair 的 generation 替换），脚本仅保留非 engine-ready 的旧 payload 兜底 | ✅ Commit 2：引擎新增 `repairWindowsGeneration`（journal Snapshot 提供旧内容恢复，pointer 保持 committed）；PS staging 仅剩非 engine-ready 兜底。注意：adapter 回滚路径上 same-version repair 的 generation 内容不再由脚本恢复——同版本内容等价 + 崩溃恢复由 journal 覆盖，这是有意取舍 |
| `Write-RuntimeManifest`（334-380）+ 调用点 1873-1893、1949-1977 | 非 engine-ready 兜底 manifest 写入（runtime.json schema、tunnel/URL/startup 投影） | 保留为非 engine-ready 兜底，但确保 engine-ready 时绝不执行（现状已如此），并把二次调用收敛为一处 | ⏳ Commit 3 |
| `Write-ActiveVersionState`（381-416） | active-version.json 写入 —— **死代码**（引擎已接管 pointer） | Commit 2 直接删除 | ✅ Commit 2：已删除，并有测试禁止回归 |
| 主流程 1707-1721 | `desktop-version.txt` 按 engineOwnsTargetGeneration 分叉写入 | 引擎在 Windows apply 内写入（payload 版本已验证）；脚本仅非 engine-ready 路径保留 | ✅ Commit 2：`activateWindows` 写入并纳入 journal snapshot |
| 主流程 1748-1893 | token 复用/生成策略、OAuth 凭据策略、server URL / named token 解析链、HKCU Run 内容投影 | 内容策略逐步收口引擎；DPAPI/HKCU 写入属 OS bridge 保留。Commit 3 仅做低风险收敛（去重、单一入口），大迁移留待后续阶段 | ⏳ Commit 3 |
| catch 块 2175-2425 | 脚本侧回滚事务（备份/恢复文件、Run value、Task、进程、generation 删除、engine abandon 编排） | 短期保留：这是 OS adapter 回滚（engine journal 覆盖不到 HKCU/Task/UAC 状态）。但 abandon 的调用必须已由引擎的 `--rollback-failed` 分类语义收口（现状满足）；后续把可引擎化的文件恢复并入 journal | ⏳ Commit 3（整理） |
| `Test-AgentDockTaskEligible`（577）+ 1272-1292 task conflict 门 | Task 归属/冲突判断 | 属 OS bridge（ScheduledTasks API），但归属判定规则与引擎 `windowsManagedTaskName` 保持单一来源；Commit 3 复核一致性 | ⏳ Commit 3 |

### uninstall-windows.ps1（Commit 4 靶子）

| 位置 | 决策 | 迁移方向 |
|---|---|---|
| 154-183 | 从 `runtime.json` 自行推导 managed task name（含 legacy 固定名回退） | 与引擎 `uninstall.go:122-136 windowsManagedTaskName` 统一：引擎在 uninstall Result 返回 task name，或脚本改用 inspect 提供的 manifest 投影 |
| 189-208 | 自读 `install\transaction.json` 判断"是否有待续 uninstall trial"，手工校验 transaction id | 扩展 `install inspect`（transaction_id/action/state），脚本改为调用 inspect 判断 resume；不再手工解析 transaction JSON |
| 318-335 | commit 时机判断（cleanup 全部成功后 commit） | 保留：这正是 defer-commit 合同的正确用法；引擎已拥有状态机 |

### install.sh（Commit 5 顺带）

- `backup_public_config`/`restore_public_config`/`commit_public_config`/`restart_restored_services`（267-323, 422-447）：env 文件级回滚事务 —— 保留（bootstrap 层最小回滚，引擎事务之外的 OS 状态）。
- `install_quick_tunnel_retry_guard`/`quick_tunnel_rate_limited`/`stop_rate_limited_tunnel`（324-432）：quick tunnel 429 限流分类与 systemd drop-in —— 属 OS bridge + 少量分类逻辑，保留但注释边界。

### install-linux-platform.sh / install-macos-platform.sh（Commit 5 / 6 靶子)

- 两个平台脚本各有 **engine 分支 + legacy fallback 分支**：
  - Linux：engine 路径 `apply_linux_with_go_installer`（750-802），legacy 路径 1565-1626（write_env_file/write_runtime_manifest/units/health/skill bootstrap/tunnel 配置）。engine 失败已 `die`（禁止回退 legacy），legacy 路径仅在非 engine-ready 旧 payload 时可达。
  - macOS：engine 分支 1520-1612，legacy 分支 1573-1631 + `skill bootstrap`（1614-1623）+ `configure_tunnel`（1625-1631）。
- 迁移方向：legacy fallback 是旧 payload 兼容路径，**公开入口和旧版升级路径必须有真实证据才能删**。Commit 5/6 先做：确认 legacy 分支的可达条件（非 engine-ready 的旧 payload 是否仍是受支持的升级起点），有证据后删除不可达代码；保留 launchd/systemd unit 生成、用户/权限 bootstrap、下载校验。
- macOS 脚本里的 tunnel rollback 状态机（`rollback_tunnel_start`/`restore_previous_public_auth` 等，932-1040）：engine 分支下这些只服务 legacy 路径；随 legacy 判定一起收口。

### manage-windows.ps1（Commit 7 靶子）

Action 盘点（native 等价物与处置）：

| Action | 现状 | native 等价 | 处置 |
|---|---|---|---|
| `start`/`stop`/`start-tunnel`/`stop-tunnel` | 转发 native | `service/tunnel start|stop` | 保留薄转发 |
| `restart` | `Restart-AgentDockRuntime`（933）残留 quick-mode URL 清理 + manifest 投影 | `service restart` + `tunnel restart` | Commit 7 收敛：quick 模式投影走 native，脚本只做组合调用 |
| `update` | 转发 `agentdock update` | 有 | 保留薄转发 |
| `set-mode` | 转发 `tunnel configure` | 有 | 保留薄转发 |
| `regenerate-quick` | 转发 `tunnel regenerate` | 有 | 保留薄转发 |
| `set-startup` | 转发 `service autostart` | 有 | 保留薄转发 |
| `launch-core` | Ensure-Credentials（DPAPI 生成策略）+ `service launch-core` | `service launch-core` | 保留：launcher 场景的 DPAPI 凭据兜底是 OS bridge |
| `launch-tunnel` | 转发 `tunnel start` | `tunnel launch` | 保留薄转发（兼容 launcher 契约） |
| `set-task-startup` | Enable/Disable Task + UAC tray `--task-admin set-enabled` | 无 CLI 等价 | 保留：Task/UAC 是 OS bridge |
| `task-run-session` | COM Schedule.Service RunEx | 无 | 保留：Setup broker 契约（launch-windows-process.ps1 依赖） |
| `task-start` | Start-TaskPreservingStartupState | 无 | 保留：OS bridge |
| `task-stop` | Stop-ScheduledTask + `service stop` | 部分 | 保留：OS bridge |

**删除条件**：某个 action 的所有调用方（含旧版本升级路径与兼容 launcher）确认不存在后，才能从 ValidateSet 冻结集中移除。当前全部保留。

## 5. 不允许回归的语义（与任务书 14 条一一对应）

1. Windows uninstall 任一关键失败不得 rc=0 假成功 —— `uninstall-windows.ps1` 保持"全部 adapter 清理成功才 commit"。
2. uninstall destructive cleanup 后失败可安全重跑 —— trial resume + transaction-id scoped retry。
3. detached helper 用 transaction id 可重入 —— `install detach-engine` + `install commit --transaction-id`。
4. helper 丢失不得假成功 —— detach 验证失败必须失败退出。
5. external rollback_failed 阻塞下一次安装 —— `install abandon --rollback-failed` 语义。
6. generation pointer two-phase crash safe —— `apply.go:752-841`。
7. failed/rolled_back 不泄漏失败 target projection —— store 投影规则。
8. terminal transaction durable authoritative —— `store.go Complete()`。
9. inspect 能修复 stale result projection —— `inspect.go RequireInspection`。
10. uninstall → reinstall source_version 保留 known-good，不得 unknown。
11. legacy v0.8.x → generation 升级保留 fallback —— `legacy_windows.go`。
12. Named Tunnel 失败回滚后 Quick/Core 恢复。
13. Windows Task ownership 不误伤别的安装/生产 Task —— 默认 startup value names 才允许动 Task。
14. production Task 在测试期间不得被污染。

新增/修改 Go API 时，每条语义必须有对应测试或真机验证覆盖。

## 6. Commit 规划

| Commit | 范围 | 本文对应 |
|---|---|---|
| 1 | 本盘点文档（不改行为） | 全文 |
| 2 | Windows generation/manifest 剩余决策迁 Go：inspect 扩展、same-version repair staging 收口、死代码删除、desktop-version.txt 引擎化 | §4 install.ps1 前 4 行 |
| 3 | install.ps1 瘦身（消费新 inspect、收敛 manifest 兜底、回滚块整理） | §4 install.ps1 其余 |
| 4 | Windows uninstall 收口：inspect 化 resume 判断、task name 单一来源 | §4 uninstall-windows.ps1 |
| 5 | Linux installer 收口：legacy 分支可达性证据与删除、保留 systemd/OpenRC bridge | §4 Linux |
| 6 | macOS installer 收口：同上 + tunnel rollback 状态机收口 | §4 macOS |
| 7 | manage-windows.ps1 legacy actions 清理 | §4 manage-windows 表 |
| 8 | 死代码与确认无调用兼容代码删除 | 全文复核 |
