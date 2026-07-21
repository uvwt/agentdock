# AgentDock Goal Mode — 工程規格

狀態：Active（完整 Goal Mode 產品可用：Dashboard/Approval/Tunnel/Desktop entry + Core Runtime）  
對應上游：AgentDock `v0.5.4+`  
原則：Goal 狀態、工具執行、驗證證據在 AgentDock；ChatGPT 只做需要模型推理的部分。

## 1. 產品定義

Goal Mode 讓使用者定義**可驗證結果**，而不是單次對話任務：

```text
使用者定義結果
→ 持久 Goal
→ Milestones / Steps
→ 本地確定性執行
→ 結構化推理提交
→ 證據驗證
→ 跨對話恢復
```

與 `task_manage` 的關係：

| | `task_manage` | Goal Mode |
|---|---|---|
| 定位 | 輕量可恢復任務 | 長跑、可驗證、可輪替 Worker 的目標 |
| 狀態 | active / blocked / completed | 完整狀態機（planning → executing → verifying …） |
| 完成條件 | 自由文字 | 可機讀 success criteria + evidence |
| 一致性 | 無 lease | Lease + capsule_version |
| 推理 | 依賴當前對話 | Capsule 可跨對話 resume |

短期兩者並存；Goal 不自動遷移舊 task。

## 2. 架構（Core-first）

```text
AgentDock Core
├── Goal Engine          internal/goal
├── Policy / Approval    （✅ P2 + 內建工具閘道）
├── Execution Router     （✅ workflow/execute_steps）
├── Evidence / Artifacts （✅ sha256 content-addressed）
├── Browser Runner       既有
└── ChatGPT Web Adapter  （P5，可替換）
```

**不做**：把 ChatGPT 對話 ID、Tab、自然語言答案當作 Goal 狀態來源。

### 階段（修正後）

| Phase | 交付 | 完成標準 |
|---|---|---|
| **P0** | Core 可被第二個 binary 初始化；Goal package 可獨立測試 | `go test ./internal/goal ./internal/tools` 通過 |
| **P1** | Goal Store + Capsule + Lease + `goal_manage` MCP | 建立 Goal → 關進程 → 重開恢復；`commit_turn` 版本衝突可攔截 |
| **P2** | Approval + Deterministic Workflow + Verifier | ✅ `check_policy` / `resolve_approval` / `verify` / `run_workflow` / `execute_steps`；`mark_completed` 需 verifier 全過 |
| **P3** | Manual cross-conversation resume | ✅ 新 Runtime 只靠 capsule/resume_prompt 繼續；`TestManualCrossConversationResume` |
| **P4** | Desktop shell（可選） | ✅ `/goal` HTML + Runtime API；❌ Wails 原生 App / Tray 仍可後置 |
| **P5** | ChatGPT Web Loop | ✅ Loop + `RuntimeBrowser`（browser_session/act）；需 BrowserEnabled 與真人登入 profile；UI 改版仍可能需調 locator |
| **P6** | Hybrid reasoning | ✅ `internal/reasoner` 路由/預算/fallback |
| **P7** | Multi-device | ✅ `internal/device` registry + handoff；❌ 尚無跨設備 Goal DB 同步 |
| **Tunnel** | Public MCP | ✅ `internal/tunnel` cloudflared/LAN/custom/loopback + `/internal/runtime/tunnel` |

## 3. 資料模型

### Goal

```json
{
  "schema_version": 1,
  "goal_id": "goal_01…",
  "title": "修復登入按鈕",
  "objective": "…",
  "status": "executing",
  "mode": "guarded",
  "workspace_id": "web-app",
  "base_git_sha": "",
  "current_git_sha": "",
  "capsule_version": 3,
  "milestones": [],
  "steps": [],
  "success_criteria": [],
  "constraints": [],
  "budget": {},
  "pending_approvals": [],
  "evidence": [],
  "active_lease": null,
  "blocker": "",
  "summary": "",
  "created_at": "…",
  "updated_at": "…"
}
```

### 狀態機

```text
draft
  → planning
  → awaiting_plan_approval
  → executing
  → verifying
      → completed
      → regressed → replanning → executing
      → blocked
      → awaiting_reasoning
```

旁路：`paused` / `cancelled` / `failed` / `awaiting_user` / `awaiting_credentials` / `awaiting_approval`

規則：

- `planning`：只讀
- `executing`：僅已批准範圍
- `verifying`：不擴大修改
- `completed`：每條 success criterion 必須有 evidence（P2 強制；P1 記錄聲明）
- `blocked`：必須含原因、已嘗試、證據、需要使用者做什麼

### Success Criteria（可機讀）

```json
{
  "id": "tests",
  "type": "command",
  "expression": "test_exit_code == 0",
  "status": "pending"
}
```

類型：`command` | `metric` | `browser` | `manual`  
P1 只存儲與 capsule 暴露；P2 Verifier 求值。

### Budget

```json
{
  "max_reasoning_turns": 20,
  "max_replans": 4,
  "max_conversation_rotations": 5,
  "max_runtime_minutes": 180,
  "max_identical_failures": 2,
  "max_browser_retries": 3,
  "max_changed_files": 20
}
```

### Lease

```json
{
  "lease_id": "lease_…",
  "goal_id": "goal_…",
  "worker_id": "chatgpt-web-session-03",
  "capsule_version": 18,
  "expires_at": "…"
}
```

`commit_turn` 必須：`goal_id` + `lease_id` + `expected_capsule_version` 全部匹配，否則 `STATE_CONFLICT`。

## 4. Goal Capsule

每次交給推理 Worker 的是固定 budget 摘要，不是完整歷史。

| 區段 | 預算（token 目標） |
|---|---:|
| Goal 與限制 | 1,500 |
| 當前狀態 | 1,500 |
| 相關程式碼 | 6,000 |
| Diff | 4,000 |
| Evidence 摘要 | 2,000 |
| 未決問題 | 1,000 |

`goal_get` 預設回 Capsule；完整 Goal 狀態僅本機 / Desktop。

## 5. MCP 表面

單一工具 `goal_manage`（與 `task_manage` 一致），actions：

| action | 說明 |
|---|---|
| `create` | 建立 Goal（draft/planning） |
| `list` | 列表 |
| `get` | 回 Capsule（可選 full） |
| `commit_turn` | 結構化推理提交 |
| `request_approval` | 登記待批准操作 |
| `update_constraints` | 更新約束 |
| `pause` / `resume` / `cancel` | 生命週期 |
| `mark_blocked` | 阻塞並要求使用者 |
| `mark_completed` | 聲明完成（P2 起強制 evidence） |
| `get_evidence` | 讀 evidence 摘要 / 引用 |
| `acquire_lease` / `release_lease` | Worker 租約 |

`commit_turn` 輸入核心：

```json
{
  "goal_id": "goal_…",
  "reasoning_lease_id": "lease_…",
  "expected_capsule_version": 18,
  "decision": "continue",
  "summary": "…",
  "next_milestone": "fix-login",
  "steps": [
    {"action": "inspect_files", "targets": ["src/login.ts"]},
    {"action": "prepare_patch"},
    {"action": "run_tests"}
  ]
}
```

只接受**已知 step action** 白名單；禁止把任意 shell 字串當 step。

## 6. 儲存

沿用 taskstate 模式（P1 不上 SQLite）：

```text
~/.agentdock/
├── goals/
│   ├── goal_….json          # 當前狀態
│   └── .store.lock
├── events/
│   └── goal_….jsonl         # append-only
└── artifacts/               # P2 content-addressed
    └── sha256/…
```

- SQLite = 未來查詢層  
- JSONL = 可稽核歷史  
- Artifact store = 大內容  

## 7. 與現有程式碼的接縫

| 既有 | 用途 |
|---|---|
| `internal/tools.Runtime` | 掛 `goals *goal.Store` |
| `internal/taskstate` | 持久化模式參考；不共用表 |
| `internal/publicartifacts` | 短期對外交付截圖/日誌 |
| `internal/tools/browser_*` | 驗證與（P5）ChatGPT adapter |
| `internal/auth` OAuth | 遠端 ChatGPT MCP |
| `atomicfile` / `filelock` | Goal 寫入 |

Phase 0 不強制大拆 `main.go`；`NewRuntime` 已可被其他 binary 呼叫。真正 Desktop 入口出現時再抽 `internal/runtimeapp`。

## 8. 安全

- Browser / 網頁內容標記 `untrusted_observation`，不得改 constraints / 自行批准
- 高風險：push、deploy、依賴安裝、刪除、上傳 → approval
- 專用 browser profile，不讀日常 Chrome profile
- Log 過濾 Authorization

## 9. 第一個 Demo（P3 完成時）

```text
/goal 修復本機網站登入按鈕
成功條件：
- unit tests 通過
- browser 登入後 URL 含 /dashboard
- console errors == 0
- 保存修改前後 screenshot
約束：不得 push；安裝依賴需批准
```

流程：create → plan → approve → patch → test → browser verify → evidence → complete；中斷後新對話 `goal_manage get` + Capsule resume。

## 10. 明確不做（MVP）

- 重寫 AgentDock 為其他語言
- 解析 ChatGPT 自然語言當 Goal 狀態
- 自動 CAPTCHA / 2FA
- 自動批准 push / deploy
- 一開始跨設備 Goal 同步
- 一開始合併 coding-tools-mcp
- 一開始上 Wails / Tunnel Manager（可後置）

## 11. 成功指標（MVP）

- Goal 重啟恢復成功率 100%（單機檔案）
- `STATE_CONFLICT` 正確攔截過期 capsule commit
- Capsule 可在無聊天歷史下恢復上下文
- 無 evidence 的 complete 在 P2 被拒絕


## 12. P2 執行表面

`goal_manage` 新增：

| action | 說明 |
|---|---|
| `check_policy` | 對 step action + targets 做能力分級判定 |
| `resolve_approval` | approved / rejected 待批准項 |
| `verify` | 以 structured evidence 重算 success criteria |
| `execute_steps` | 執行 Goal 內 pending 白名單步驟（可本地化的） |
| `run_workflow` | 執行確定性 workflow（run / verify_* / artifact / sleep） |
| `add_evidence.evidence_data` | 供 verifier 使用的 machine fields |

Policy 分級：`auto` / `goal_auth` / `approve` / `forbid`。  
約束 `no_git_push`、`dependency_install_requires_approval` 等會攔截或要求批准。

Verifier 支援：

- command：`test_exit_code == 0` / `exit_code == 0`
- browser：`url_contains:/dashboard`、`console_errors == 0`
- metric：`fps_median >= 29.5`
- manual：需帶 `criterion_id` 的 evidence

`mark_completed` 在任一 criterion 未 satisfied 時回 `VERIFY_FAILED`。


## 13. P2/P3 補齊

### 高風險工具閘道
- `goal_manage create` / `bind` 會設定 Runtime `active_goal_id`
- 綁定期間 `exec_command` / `file_edit` / `git_write` 經 `CheckPolicy`
- `unbind` 解除閘道（回到非 Goal 自由模式）
- planning 狀態禁止 mutating 執行；需先 `commit_turn` 進入 executing

### Content-addressed Artifacts
```text
~/.agentdock/artifacts/sha256/ab/<hex>
~/.agentdock/artifacts/sha256/ab/<hex>.json
```
URI：`artifact://sha256/<hex>`  
`goal_manage store_artifact` 可寫入並可選掛到 Goal evidence。

### 手動跨對話 Resume
1. 對話 A：create → acquire_lease → commit_turn → store_artifact  
2. 關閉 Runtime（模擬新對話）  
3. 對話 B：get capsule → bind → acquire_lease → commit_turn → evidence → mark_completed  
不需要任何對話 A 的聊天歷史。


## 14. Progress / Dashboard / Web Adapter

### Progress Detector
- `ProgressFingerprint` 對 evidence / criteria / steps / milestones / problem 做雜湊
- 連續 `max_identical_failures`（預設 2）輪無進展 → `status=blocked` reason `no_progress`
- 禁止只靠「繼續」空轉

### Operator Dashboard（非 Wails）
- HTML：`GET /goal`, `GET /goal/{id}`
- JSON：`GET /internal/runtime/goals`, `GET /internal/runtime/goals/{id}`
- 批准：`POST /internal/runtime/goals/{id}/approvals/{approval_id}`

### ChatGPT Web Adapter
- 套件：`internal/chatgpt`
- `Loop.Wake` 只投遞 Capsule `resume_prompt`，**不解析**聊天自然語言
- 輪替觸發：turn limit / quota / page error
- 真實 DOM 自動化需實作 `chatgpt.Browser`（可包 browser_session/act）


## 15. Tunnel / Hybrid / Devices

### Tunnel Manager (`internal/tunnel`)
- Modes: `disabled` | `loopback` | `lan` | `cloudflare` | `custom`
- Config: `~/.agentdock/tunnel/config.json`
- Cloudflare quick tunnel parses `*.trycloudflare.com`; named tunnel via token
- API: `GET /internal/runtime/tunnel`, `POST .../start`, `POST .../stop`

### Hybrid Reasoner (`internal/reasoner`)
- Backends: `local` | `api` | `chatgpt_web`
- Class routing: log→local, fix→api, architecture/review→chatgpt_web
- Budget + fallback chain; **does not** call models itself (executor injected)

### Device Registry (`internal/device`)
- `~/.agentdock/devices/registry.json`
- Upsert devices, create/update handoffs
- Explicitly **not** a replicated Goal store; handoff is bookkeeping for Phase 7

### ChatGPT RuntimeBrowser (`internal/chatgpt`)
- Implements `Browser` via AgentDock `browser_*` tools
- Locators isolated from Goal Core
- Requires browser enabled + interactive login to chatgpt profile


## 16. Desktop entry & browser bundle

```bash
# operator desktop-style entry (opens /goal)
go run ./cmd/agentdock-desktop

# install browser runner + detect Chrome/Chromium
./scripts/bundle-browser-desktop.sh
source ~/.agentdock/browser/env.sh
```

Shared boot path: `internal/runtimeapp.Run` (CLI + desktop).
Wails can later wrap the same core; HTML dashboard is the current UI shell.

### Multi-device remote client
`internal/device.RemoteClient` fetches `/internal/runtime/goals/{id}` from another host and `ImportCapsule` seeds a local goal for handoff.


## 17. 產品完成定義

使用者可：

1. `go run ./cmd/agentdock-desktop` 啟動
2. 在 `/goal` 查看 Goal、批准高風險操作、Pause/Resume/Cancel
3. 透過 MCP `goal_manage` 建立/恢復/驗證 Goal
4. 使用 Tunnel 面板暴露 MCP URL 給 ChatGPT Web
5. 跨對話只靠 Capsule resume，不靠聊天歷史

產品文件：`docs/goal-mode-product.md`


## 18. ChatGPT Browser Worker

- Package: `internal/chatgpt` (`Worker`, `Loop`, `RuntimeBrowser`)
- Runtime field: `Runtime.chatgptWorker`
- Auto-wake on `RequestReasoning` → status `awaiting_reasoning`
- Dashboard: wake button + worker panel
- API: `POST /internal/runtime/goals/{id}/chatgpt_wake`, `GET /internal/runtime/chatgpt/worker`
- Rotation: turn limit / quota / page error via `Loop.ShouldRotate`


## 19. L3 Goal Orchestrator

- Package: `internal/orchestrator`
- Runtime: `Runtime.orch` via `ensureOrchestrator()`
- MCP: `orchestrate_start` | `orchestrate_stop` | `orchestrate_status`
- Dashboard buttons on goal detail
- Loop: wake → wait commit → execute → verify → rotate/block/complete
- Safety: MaxNoCommit, MaxTicks, human gates (approval/pause), progress/blocked respect
