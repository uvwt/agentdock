# AgentDock Goal Mode — 產品使用指南

**唯一使用者介面：自動化 Chromium 裡的 ChatGPT 網頁。**  
沒有操作台 / Dashboard。AgentDock 只提供 MCP、Goal 狀態、本地工具與自動回灌。

## 閉環

```text
ChatGPT 網頁
  → MCP（goal_manage / 工具）
  → 本地電腦執行
  → 結果寫入 Goal / evidence
  → 需要推理或任務結束前總結時
  → Browser Worker 把 resume/summary 貼回 ChatGPT 網頁
  → 直到 Goal completed / blocked
```

## 啟動

```bash
./scripts/bundle-browser-desktop.sh
source ~/.agentdock/browser/env.sh

# 產品入口：設定 Console + MCP + 自動開 ChatGPT（專用 profile）
go run ./cmd/agentdock-desktop
# 或
./bin/agentdock-desktop
```

啟動後會打開 **設定 Console**：`http://127.0.0.1:8765/console`

| 頁面 | 用途 |
|---|---|
| `/console` | **產品 Web UI**：MCP 位址、Tunnel、ChatGPT 瀏覽器、Runtime 狀態 |
| ChatGPT 網頁 | 真正下 Goal / 長任務推理與工具呼叫 |
| CLI / curl | 進階除錯 |

Console **不是** Goal 任務看板；它只簡化連線與設定。

輸出示例：

```text
ChatGPT: opened dedicated browser profile (chatgpt)
MCP:     http://127.0.0.1:8765/mcp
Loop:    ChatGPT web → MCP → local tools → resume/summary back to ChatGPT
```

第一次在跳出的瀏覽器視窗**手動登入 ChatGPT**（CAPTCHA / 2FA）。之後同一 `chatgpt` profile 會沿用。

## 在 ChatGPT 裡怎麼用

1. 把 MCP 指到 `http://127.0.0.1:8765/mcp`（雲端 ChatGPT 需 Tunnel / 公開 HTTPS）
2. 下目標，例如：

```text
用 goal_manage 建立 Goal：修復登入，成功條件 tests 通過與 browser 進入 /dashboard。
約束：不得 push。然後 orchestrate_start 無人值守直到完成。
```

3. 模型應呼叫：
   - `goal_manage create`
   - `goal_manage orchestrate_start`（L3 閉環）
   - 中間 `commit_turn`、本地工具、`add_evidence`
   - 完成前總結並 `mark_completed`（有 evidence）

## CLI 除錯（可選）

```bash
# 狀態
curl -s http://127.0.0.1:8765/healthz
curl -s http://127.0.0.1:8765/internal/runtime/goals
curl -s http://127.0.0.1:8765/internal/runtime/chatgpt/worker

# 手動喚醒 / 編排
curl -s -X POST http://127.0.0.1:8765/internal/runtime/goals/<id>/chatgpt_wake
curl -s -X POST http://127.0.0.1:8765/internal/runtime/goals/<id>/orchestrate_start
curl -s -X POST http://127.0.0.1:8765/internal/runtime/goals/<id>/orchestrate_status
```

## 明確不做

- 不提供 Goal Dashboard / 操作台
- 不解析 ChatGPT 聊天文字當 Goal 狀態
- 不自動過 CAPTCHA / 2FA
- 不把完整 log 灌進 ChatGPT（只貼 capsule / 短摘要）

## 架構

```text
ChatGPT 網頁 = 唯一 UI 與推理
AgentDock    = 隱形 Runtime（狀態、工具、編排、回灌）
```
