# Installer 脚本职责盘点（第二、三阶段收口）

基线：`fix/installer-engine-mini-20260913`，起点 `449adba`，对照 `origin/main` @ `f1dde3a`。

本文记录 Installer Engine 第二阶段脚本瘦身，以及第三阶段 Unix 公开入口去平台脚本依赖的收口结果。它包含：

1. 各脚本的职责分类与真实调用图；
2. Go Installer Engine 已经拥有的决策；
3. 本次收口迁移/删除的内容与理由；
4. `manage-windows.ps1` 逐 Action 处置与删除条件；
5. install.ps1 剩余大块的去留说明；
6. 不允许回归的语义清单。

文档开头的行号以起点 `449adba` 为准，函数名是稳定锚点。

## 1. 脚本规模变化

| 脚本 | 起点（449adba） | 收口后 | 变化 |
|---|---|---|---|
| `scripts/install/install.ps1` | 2431 | ~2373 | 决策迁引擎；manifest 兜底收敛 |
| `scripts/install/manage-windows.ps1` | 1017 | ~875 | restart 委托原生；孤儿函数删除 |
| `scripts/install/uninstall-windows.ps1` | 336 | ~358 | resume 决策迁引擎（结构化恢复 bootstrap） |
| `scripts/install/launch-windows-process.ps1` | 220 | 220 | 不变（薄 broker，保留） |
| `scripts/install/probe-protected-text.ps1` | — | 35 | 最新 main 新增的只读 DPAPI 可用性探针，纳入脚本治理 |
| `scripts/install/install.sh` | 685 | 634 | 第三阶段改为直接下载 Release payload 并调用 Go Engine，不再下载/执行 platform installer |
| `scripts/install/install-linux-platform.sh` | 1702 | 1150 | 删除 legacy 安装状态机 |
| `scripts/install/install-macos-platform.sh` | 1701 | 949 | 删除 legacy 安装状态机 |
| `scripts/install/uninstall-linux.sh` | 401 | 401 | 不变（已委托 engine） |
| `scripts/install/uninstall-macos.sh` | 125 | 125 | 不变（已委托 engine） |

治理基线见 `scripts/governance/inventory.yaml` 与 `scripts/test/script_governance_test.go`：
manage-windows.ps1 的 ValidateSet action 集保持冻结，未新增调用方。

## 2. 调用图（真实调用方）

### install.ps1
- `packaging/windows/AgentDock.iss:69,75`：随 Setup 打包（`{app}\installer`）。
- `packaging/windows/includes/code.iss:461-502`：Inno `PrepareToInstall` 调用；`:513-542` 解析结果 INI。
- `.github/workflows/release.yml:426`：发布到 `dist/install.ps1`（公开下载入口，release smoke 字节对比）。
- `.github/workflows/windows-installer.yml:70-74,238,249`：CI E2E。
- `scripts/test/test-install-windows-e2e.ps1:205-222`、`test-windows-quick-tunnel-lifecycle.ps1:155-170`、`test-windows-setup-activation-deferred.ps1:71-88`。

### manage-windows.ps1
- 随 release zip 发布（`release.yml:311-312`）与 Setup payload 打包（`AgentDock.iss:71`、`code.iss:441`）。
- Go 侧发布：`internal/installer/apply.go`（→ `{install-root}\installer\`）、`internal/selfupdate/desktop_apply_windows.go`（自更新 staging）。**它是运行时 payload 的一部分，不能删除，只能逐 Action 退役。**
- 脚本调用点：`install.ps1`（`task-run-session`）、`launch-windows-process.ps1`（`task-run-session`）、生成的 start-cloudflared.ps1 兼容 launcher。
- 测试调用：`test-install-windows-e2e.ps1`（`restart`）、`test-windows-task-scheduler.ps1`（`task-run-session`）、`test-windows-setup-e2e.ps1`。

### uninstall-windows.ps1
- `packaging/windows/includes/code.iss:582-617`（`-KeepInstallDir` / `-PurgeState`）。
- `.github/workflows/release.yml:427`；`test-windows-quick-tunnel-lifecycle.ps1:358-362`。

### install.sh / 平台脚本 / 卸载脚本
- `install.sh`：Unix 唯一公开 bootstrap；Makefile 的 `install` / `install-linux` / `install-macos` 都指向它，release 与真实 E2E 也走同一入口。
- `install-linux-platform.sh` / `install-macos-platform.sh`：保留为已经发布的独立 adapter 契约，但 `install.sh`、Makefile 和新 E2E 不再调用它们。
- `uninstall-linux.sh` / `uninstall-macos.sh`：保留独立卸载契约；统一入口的 `--uninstall` 已直接调用 Go Installer Engine，不再下载/转发这些脚本。

## 3. Go Installer Engine 拥有的决策（单一权威，禁止脚本重复实现）

`internal/installer/`：

| 能力 | 位置 |
|---|---|
| install/repair/uninstall/commit/abandon 事务与中断恢复 | `engine.go` |
| Windows generation 发布 / attach / same-version repair 重建 / active-version.json 两阶段 pointer | `apply.go` |
| runtime.json manifest 生成（engine-ready 路径） | `apply.go` |
| desktop-version.txt 版本投影（engine-ready 路径） | `apply.go activateWindows` |
| legacy → generation 一次性迁移 | `legacy_windows.go`（CLI：`install prepare-windows-legacy`） |
| rollback journal 与有序恢复 | `journal.go` |
| 服务/tunnel 生命周期（systemd/OpenRC/launchd/Windows native CLI） | `service.go` |
| uninstall defer-commit + trial 重绑定 + detach-engine 辅助 | `engine.go`、`cmd/agentdock/command_install_windows.go` |
| inspect 权威投影 + pointer/pending-update 评估 | `inspect.go` |
| known-good 判定 / Windows pointer 对账 | `engine.go` |
| systemd/OpenRC/launchd 单元模板与日志治理 | `units.go` |
| Quick Tunnel 地址回写（unix） | `internal/desktopruntime/tunnel_unix.go runQuickTunnel` |

脚本侧引擎调用合同：`install --engine-ready` 探针、`install inspect`、`install --defer-commit ...`、
`install commit/abandon`、`uninstall --defer-commit`、`install detach-engine`、`install prepare-windows-legacy`。

## 4. 本次收口迁移/删除记录

### install.ps1

| 决策点 | 处置 | 理由 |
|---|---|---|
| 手工解析 `active-version.json` + update-transaction 门 | ✅ 迁 Go | `install inspect` 新增 `pointer_state` / `pointer_active_version` / `pointer_fallback_version` / `pending_update_transaction` / `transaction_id` / `action`；脚本消费结构化结论，仅在非 committed 时执行"运行 stable binary 触发恢复"这一 OS 动作后重新 inspect |
| legacy 判定（是否需要 bootstrap source generation） | ✅ 迁 Go | legacy 门改用 `pointer_state -eq 'missing'`；`prepare-windows-legacy` 幂等（`already_ready`），脚本不再自行判定 |
| `$engineOwnsTargetGeneration` 布尔组合 | ✅ 删除 | generation 归属决策全部在 `stageWindowsPayload` 内部；脚本只剩 `-not $engineReady` 能力判断 |
| PowerShell 侧 same-version repair staging（`.repair-backup`） | ✅ 迁 Go | 引擎新增 `repairWindowsGeneration`：journal Snapshot 提供旧内容恢复，pointer 保持 committed；脚本 staging 仅剩非 engine-ready 旧 payload 兜底 |
| `Write-ActiveVersionState` | ✅ 删除 | 死代码；active-version.json 由引擎唯一拥有（测试禁止回归） |
| `desktop-version.txt` 双路径写入 | ✅ 迁 Go | `activateWindows` 统一写入并纳入 journal；脚本仅非 engine-ready 路径保留 |
| 两处 `Write-RuntimeManifest` 兜底调用 | ✅ 收敛为一处 | 唯一 `-not $engineReady` 入口；engine-ready 时 runtime.json 由引擎生成 |

### uninstall-windows.ps1

| 决策点 | 处置 | 理由 |
|---|---|---|
| 自读 `install\transaction.json` 判断 resume | ✅ 迁 Go（主体） | 引擎 `uninstall` 按 transaction id 重绑定 pending trial；stable binary 存在时脚本完全不再判断 resume |
| managed task name 解析 | ✅ 单一来源化 | uninstall Result 新增 `task_name`（引擎解析后返回）；脚本 manifest 读取仅作引擎未运行时 fallback，legacy 固定名认领规则保留在 adapter（Task 归属判断） |
| 无 stable binary 的恢复路径 | ✅ 保留（最小化） | durable transaction 是定位 txid-scoped helper 的唯一恢复线索（irreducible bootstrap）：trial 必须找到 helper（丢失不得假成功）、committed 幂等清理、其余状态由引擎在 commit 时拒绝。状态变更决策仍全部在引擎 |
| cleanup 全部成功后 commit | 保留 | defer-commit 合同的正确用法 |

### install-linux-platform.sh / install-macos-platform.sh

- **删除全部 legacy 安装状态机**（env/manifest/unit/tunnel 编排/health/skill bootstrap/rollback）。
- **第二阶段可达性结论仍成立**：platform adapter 已只支持 Engine-ready payload。第三阶段进一步移除了统一入口到这些 adapter 的运行时依赖；`install.sh` 直接下载同一 Release 的 `agentdock_<os>_<arch>.tar.gz`，校验后执行其中的 `install --engine-ready` / `install` / `uninstall`。
- 保留：下载与 SHA-256 校验、用户/权限 bootstrap、cloudflared staging、安装后信息摘要、（macOS）LaunchAgent 预态安全检查与结果文件输出。
- 配套：OpenRC/plist 日志治理断言改指引擎 `internal/installer/units.go` 权威模板；Quick Tunnel 地址回写由原生 `runQuickTunnel` 拥有，新增 `internal/desktopruntime/tunnel_quick_unix_test.go` 回归覆盖（fake cloudflared + fake launchctl + httptest 健康端点）。

### install.sh（第三阶段）
- 删除平台 installer 下载、checksum、dispatch 与旧 shell 状态机；统一入口只做平台/架构识别、Release payload + cloudflared bootstrap、必要的 service-user/权限前置，然后直接调用 Go Installer Engine。
- install/repair 的既有 Tunnel 配置保留由 `hydrateExistingRuntime` 在 Engine 内完成，shell 不再重复解析/决定。
- `--uninstall` 直接调用 Engine；Linux purge-data 的 `AGENTDOCK_HOME`/默认工作目录由隔离 `DATA_DIR` 确定性派生并作为事务意图冻结，不再由 shell `rm -rf`。
- macOS 保持既有“卸载 App/运行支持文件、默认保留用户状态”的产品语义。自定义或继承运行时环境执行 `--purge-data` 时必须显式给出 installer cleanup 路径，禁止把宿主 `AGENTDOCK_HOME` / `AGENTDOCK_DEFAULT_DIR` 自动升级为递归删除目标；普通卸载的 install/runtime/log/App 递归删除目标也必须先通过安全路径校验，App 必须是 `.app`。
- Engine 对 `purge-data` 的用户态清理路径做第二层保护：拒绝文件系统根、系统顶层目录、整个用户主目录以及包含 install/runtime root 的目标，并把清理路径冻结进 uninstall transaction，retry 不允许漂移。
- `test-install-entry.sh` 的 fake release 只提供 tarball，不提供任何 platform installer/uninstaller 资产，以此证明统一入口不存在隐式依赖。

### manage-windows.ps1

Action 处置：

| Action | 处置 |
|---|---|
| `start`/`stop`/`start-tunnel`/`stop-tunnel`/`update`/`set-mode`/`regenerate-quick`/`set-startup`/`launch-tunnel` | 保留薄转发（native 等价命令存在） |
| `restart` | ✅ Commit 7：quick 模式委托原生 `tunnel restart`（`regenerateQuickTunnel` 内部已含清地址/manifest 投影/Core 重启），脚本只做组合调用与就绪等待 |
| `launch-core` | 保留：DPAPI 凭据兜底是 OS bridge |
| `set-task-startup`/`task-run-session`/`task-start`/`task-stop` | 保留：Task/UAC/session 是 OS bridge，无 CLI 等价 |

已删除函数（全部无调用方，旧版本使用它们自己的脚本副本）：`Write-Launchers`、`Update-RuntimeManifest`、
`Clear-ActivePublicUrl`、`Set-ObjectProperty`、`Write-JsonAtomically`、`Read-ProtectedText`、
`Read-SecretFile`、`Restart-Core`、`Stop-ProcessesAtPath`。

## 5. install.ps1 剩余大块说明（为什么必须留在 PowerShell）

当前约 2370 行，剩余每一大块的归属理由：

1. **参数解析与校验（~150 行）**：Setup/独立入口的公开参数契约（Inno 传参），入参验证是 adapter 入口职责。
2. **下载/离线 payload bootstrap（~250 行）**：release 下载、SHA-256 校验、离线 archive 解包、cloudflared staging——引擎运行之前的 bootstrap。
3. **payload 完整性预检（~100 行）**：解包后、停止旧进程前验证 payload（WSL helper manifest、per-arch hash、preflight version）。这一步必须在停进程之前做，属于 adapter 安全属性。
4. **进程/Task/Registry OS bridge（~500 行）**：Win32_Process 按路径找进程、优雅停止（15s deadline）、HKCU Run 读写、ScheduledTask 状态/冲突/UAC（`--task-admin`）、session 身份。这些是真正的 OS 操作，Go 引擎通过 native CLI 只覆盖进程生命周期，不覆盖 Task/Registry/UAC。
5. **DPAPI 凭据与 tunnel 模式解析（~250 行）**：auth token / OAuth password / tunnel token 的 DPAPI 持久化与复用（arg > env > DPAPI > 生成）、named server URL 策略。内容策略与 DPAPI 边界强耦合；后续若引擎原生接管 DPAPI（Windows CDPAPI）可再迁，本阶段保留。
6. **generation 状态消费与恢复执行（~150 行）**：消费 `install inspect` 结构化结论；触发 self-update 恢复（运行 stable binary）是执行动作不是状态判断。
7. **引擎调用与 commit/abandon 编排（~150 行）**：`--defer-commit` 安装、事务 id 握手、成功 commit、失败 abandon（含 `--rollback-failed` 分类）——defer-commit 合同的 adapter 侧。
8. **激活阶段（~150 行）**：非 engine-ready 兜底的 native service/tunnel 启动 + 健康等待 + Quick URL 读取；engine-ready 时只读 quick-tunnel-url.txt。setup 通道经 launch-windows-process broker 启动，保持 Setup 进程树与长驻运行时隔离。
9. **catch 回滚块（~250 行）**：adapter 外部状态回滚（文件备份恢复、Run value、Task restore + 恢复材料保全、进程重启、健康等待），然后 abandon。engine journal 覆盖不到 HKCU/Task/UAC 状态；这是 OS adapter 回滚的正当职责。可引擎化的文件恢复已随 generation 修复迁入 journal。
10. **结果 INI 与输出（~150 行）**：Inno 结果文件（UTF-16 INI）与用户输出——公开契约。

## 6. 不允许回归的语义（与任务书 14 条一一对应）

1. Windows uninstall 任一关键失败不得 rc=0 假成功 —— 保持"全部 adapter 清理成功才 commit"。
2. uninstall destructive cleanup 后失败可安全重跑 —— 引擎 trial 重绑定 + transaction-id scoped retry。
3. detached helper 用 transaction id 可重入 —— `install detach-engine` + `install commit --transaction-id`，helper 路径确定性。
4. helper 丢失不得假成功 —— detach 验证失败/恢复路径 helper 缺失必须显式失败。
5. external rollback_failed 阻塞下一次安装 —— `install abandon --rollback-failed` 语义。
6. generation pointer two-phase crash safe —— `apply.go` pointer trial/commit/release。
7. failed/rolled_back 不泄漏失败 target projection —— store 投影规则。
8. terminal transaction durable authoritative —— `store.go Complete()`。
9. inspect 能修复 stale result projection —— `inspect.go RequireInspection`。
10. uninstall → reinstall source_version 保留 known-good，不得 unknown。
11. legacy v0.8.x → generation 升级保留 fallback —— `legacy_windows.go`。
12. Named Tunnel 失败回滚后 Quick/Core 恢复。
13. Windows Task ownership 不误伤别的安装/生产 Task —— 默认 startup value names 才允许动 Task。
14. production Task 在测试期间不得被污染。

## 7. 收口提交序列与真机验证

| Commit | 内容 |
|---|---|
| 1 | 本盘点文档（不改行为） |
| 2 | Windows generation/manifest 决策迁 Go：inspect 扩展、same-version repair 收口、死代码删除、desktop-version.txt 引擎化 |
| 3 | install.ps1 兜底 manifest 收敛 |
| 4 | Windows uninstall：trial 重绑定 + task_name 单一来源 |
| 5 | Linux 平台脚本 legacy 状态机删除 |
| 6 | macOS 平台脚本 legacy 状态机删除 + runQuickTunnel Go 回归 |
| 7 | manage-windows.ps1 restart 委托原生 + 孤儿删除 |
| 8 | 残留死代码清扫与本文档定稿 |
| 9 | 三个真机验证修复（见下） |

### 真机验证（天翼云电脑，中文 Windows Server 2022，2026-09-13）

构建：分支源码经 file_publish 传至天翼本机构建（Go 1.27 + dotnet 8 + wsl-helper payload + core-skills bundle），payload zip 90MB。

| 验证 | 结果 |
|---|---|
| PowerShell 5.1 Parser::ParseFile × 4 脚本 | 全部通过（发现并修复 install.ps1 解析失败，见修复 1） |
| Windows 专属契约测试（`go test ./scripts/test` 在真机） | 全部通过（ASCII 扫描、内容断言、治理冻结） |
| E2E fresh install（隔离 root、自定义启动标识、18765） | committed + healthy 200 + 启动标识正确 + inspect 新字段生效 |
| E2E same-version repair | committed + 无 .repair-backup/.bootstrap 残留 + pointer committed |
| E2E 卸载失败回滚 | start_failed → rolled_back，versions/bin/runtime.json 全净，无失败目标泄漏 |
| E2E 卸载矩阵：正常卸载 | committed，binary/Run 值清理干净 |
| E2E 卸载矩阵：重装 source_version | v0.8.3（非 unknown） |
| E2E 卸载矩阵：trial 重入（binary 已删） | 同一 transaction 重绑定 → committed → helper 清理 |
| E2E 卸载矩阵：helper 丢失 | 响亮失败，trial 保留待恢复，无假成功 |
| 生产保护 | healthz 8765 / \AgentDock Task XML SHA256（UTF-16LE=cf3042fd… 与任务书一致）/ 其余 3 Task / HKCU Run / service status 前后完全一致 |

### 真机验证发现并修复的问题

1. **无 BOM 脚本的中文注释破坏 PS 5.1 解析**：install.ps1/uninstall-windows.ps1 按约定为 BOM-less，PS 5.1 按 ANSI/GBK 解码，行尾中文字符的 UTF-8 尾字节与换行配对，可吞掉换行或结构字符。install.ps1 直接解析失败（Parser::ParseFile 发现），uninstall-windows.ps1 静默吞掉一行赋值（GBK 区域设置专属，英文区域 CI 不触发）。修复：两文件新增注释全部转英文，保持 BOM-less ASCII 约定（d3141dc、e40e533）。
2. **uninstall Result `task_name` omitempty + Set-StrictMode**：standard 模式卸载时引擎 Result 不含 task_name 字段，直接属性访问抛异常导致卸载失败。改为 PSObject.Properties 安全访问（d5ae5fe）。
3. **同机并行安装的启动竞态（观察项，未在本分支修复）**：既有安装的 core 被停止后、新 core 绑定同端口前的窗口期与引擎 45s 健康等待存在竞争，重复安装偶发 start_failed（回滚路径正确）。E2E 通过安装前预清理 + 独立 `AGENTDOCK_HOME` 隔离规避。该竞态位于未改动的 OS bridge 停止逻辑，留待后续阶段评估（如把 Windows adapter 的停止等待与引擎健康窗口拉通）。
