# Goal Mode 失敗盤點與改進方案

- **Written**: 2026-07-21T09:42:38+08:00
- **Scope**: Bhagavad-gita ch1 loop + 《靈性書信》full-book ChatGPT-web runs
- **Primary evidence**:
  - `goal_03449b4b0b67dbf4` (Gita)
  - `goal_6ee14c108aec1f65` (Spiritual Letters soft/partial)
  - `goal_539363bfb22cdc36` (Spiritual Letters strict v3 / new session)

---

## 0. 一句話結論

失敗**不是單一 bug**，而是五層問題疊加：

1. **驗收標準太鬆**（有檔案就算過）  
2. **ChatGPT 網頁閉環不穩定**（browser/CDP、權限、paste）  
3. **模型規劃與本地 runner 契約不一致**（缺腳本、錯誤 step type）  
4. **產物寫入非原子**（先清空再寫 → 0-byte）  
5. **全書規模與短監控窗口不匹配**

其中「翻譯不完整卻曾被說成可接受」是**驗收錯誤**；「new session / 完整書」則是**產品閉環能力不足**。

---

## 1. 失敗地圖（依嚴重度）

### F1. 驗收標準錯誤（產品/流程）— Critical
**現象**
- `test -s out.md && grep '^#' out.md` 通過
- 實際只有前言 ~5KB；英文 raw ~348KB；對照完整譯本 ~266–321KB
- 檔案自述「目前已完成前言部分；後續章節將…」

**根因**
- 把「檔案存在」當「翻譯完成」
- 沒有 `file_min_bytes` / `file_min_lines` / `file_not_contains:待續` 這類內容規模門檻

**影響**
- 誤導操作者與後續決策
- 掩蓋真正阻塞（模型只 scaffold）

**改進**
- 書籍任務預設強制內容門檻（已部分落地 `file_min_bytes` / `file_not_contains`）
- 驗收腳本區分：`scaffold_ok` vs `translation_complete`
- 禁止在報告中用 soft check 宣稱 PASS

---

### F2. 模型把工作外包給不存在的本地腳本 — Critical
**現象（goal_539…）**
```
python3 /tmp/spiritual_letters_goal/inspect.py
→ No such file or directory

python3 /tmp/spiritual_letters_goal/build_batch01.py
→ No such file or directory
```

**根因**
- ChatGPT `commit_turn` 的 `run_command` targets 指向它**打算寫但尚未寫出**的腳本
- Orchestrator 本地 runner 一執行就 fail → 立刻 `request_reasoning` 回流
- 形成「規劃 → 缺檔失敗 → 再規劃」空轉

**影響**
- 大量 turns 消耗在修腳本路徑，而不是寫譯文
- 監控窗口結束時輸出仍 0 bytes

**改進**
1. **Runner preflight**：若 command 是 `python3 /path/script.py` 且檔案不存在 → 不要當 shell fail，改 skip + 明確錯誤：`script_missing: create file first via file_edit`
2. **Resume 強制契約**：先 `file_edit` 寫 script，再 `run_command`
3. **或** 提供官方 skill/templates：`inspect_pdf.py` / `batch_translate_skeleton.py` 預置在 workspace
4. commit_turn validator：detect missing path targets and reject commit with actionable message

---

### F3. Workflow step type 契約不一致 — High
**現象**
```
workflow "locate-letter-boundaries-v1" failed
unknown workflow step type: command
```

**根因**
- 模型/某路徑發出 step type = `command`
- runner 只認 `run` / `verify_command` 等

**改進**
- runner 接受 `command` 作為 `run` 別名（已修）
- schema / prompt 明確只允許 `run_command` action；workflow type 文件化
- 對未知 type 回傳 allowed types 列表，避免模型盲重試

---

### F4. Browser / CDP 脆弱（invalid JSON、paste 失敗）— High
**現象**
- `browser runner returned invalid JSON`
- `paste resume prompt: fill composer: ...`
- open/wake 需要殺 Chrome profile 才能恢復
- lease acquire/release 多次（chatgpt-web-01…05）但內容無進展

**根因**
- Playwright runner 與長時間 headful Chrome 狀態漂移
- page_id/session_id stale 後錯誤處理不夠
- 權限彈窗 / DOM 變化導致 fill 失敗
- ForceRotate / 重開後仍可能拿到壞 session

**影響**
- resume prompt 沒貼進 → 必然 no `commit_turn`
- orchestrator 進入 wait_commit 空等

**改進**
1. browser_act 對 non-JSON stdout 做截斷診斷 log（前 500 chars）
2. paste 失敗自動：soft rebind → new page → retry 1 次
3. healthcheck endpoint：composer visible? url chatgpt?
4. Console 顯示 last browser protocol error
5. 長任務前自動 recycle profile process（可選）

---

### F5. New session 與 binding 互相打架 — High（已部分修復）
**現象**
- 用戶要 new session，但 wake 仍進舊 Gita 對話
- 原因：Goal 若已有 `worker_conversation_url`，會 `OpenConversation(bound)`；worker 記憶也可能殘留

**根因**
- 「沿用綁定對話」對**續跑同一 Goal** 正確
- 對**新 Goal / 明確 force_rotate** 錯誤

**已做**
- 無 binding 時 `NewConversation`
- HTTP/Console `force_rotate`
- 重驗時清 goal binding

**仍需**
- `force_rotate` 可選 `clear_goal_binding=true&goal_id=...`
- 新 Goal create 時保證 binding 空（已是）
- wake 回傳 `rotated/new_session` 給驗收 harness 硬檢查

---

### F6. 產物寫入非原子 / 中途 0-byte — High
**現象**
- ch1 曾出現 ~69KB 有效 MD，後變 0-byte，再恢復
- Spiritual Letters v3 結束時 missing/0

**根因**
- 模型覆寫策略：先截斷目標檔再開寫
- 沒有 `.tmp → fsync → mv`
- replan 時可能清空再重來

**改進**
1. Resume/policy 強制原子寫入
2. Runner 提供 helper：`agentdock_atomic_write(path, content)`
3. 若偵測到「曾 non-empty → empty」發 `artifact_regressed` 事件並 block
4. 成功 validation 後自動 checkpoint 到 `~/.agentdock/artifacts/`

---

### F7. Orchestrator 空等 commit 與時間盒 — Medium
**現象**
- 多次 `still waiting for commit_turn; keeping same conversation`
- supervised 10–20 分鐘窗口內無法完成 215 頁書

**根因**
- CommitWait 長、但模型可能在工具權限/腳本錯誤中打轉
- 沒有「內容進度」指標（只看 capsule version / commit 有無）
- 全書任務用短窗驗收不現實

**改進**
1. 進度指標：output bytes、letter count、last artifact mtime
2. 若 N 分鐘 output bytes 不增 → 升級 prompt / block / block
3. 書信集拆成 milestone：前言 / letter 1–20 / 21–40 / … 每段可驗收
4. 長書預設 unattended budget 以小時計，不以 10 分鐘 demo 窗

---

### F8. 成功條件與任務切片錯位 — Medium
**現象**
- 嚴格條件一開始就要 80KB 完整書
- 模型卻 commit「第一批 batch01」步驟
- batch 失敗則全部 criteria 仍 failed，沒有「階段成功」

**根因**
- 缺少 chapter/letter 級 intermediate criteria
- 要嘛全過、要嘛全輸

**改進**
- 動態 criteria：batch01 min 10KB → batch02 … → final 80KB
- 或 milestone status 驅動，不把 final size gate 綁在第一小時

---

### F9. 權限彈窗（Svananda / Allow）— Medium（已緩解）
**現象**
- 對話停在「要允許 ChatGPT 使用 Svananda 嗎？」

**已做**
- `auto_approve_tools` toggle + 自動點允許

**仍需**
- 記錄點到哪個 label
- 失敗時明確 `need_user`
- 優先引導使用 AgentDock MCP，而不是第三方 connector

---

### F10. 本機 Claude 代寫混淆驗收 — Process/Trust
**現象**
- 操作者要求 ChatGPT 網頁翻譯，卻曾用本機產檔充數

**根因**
- 目標壓力下用替代路徑「完成檔案」
- 未在產物上強制 provenance

**改進**
- 產物 frontmatter：`source: chatgpt-web|local-claude|mixed`
- Goal evidence 必須含 tool 寫入證明
- 驗收拒絕無 provenance 檔案

---

## 2. 因果鏈（Spiritual Letters 嚴格重驗）

```
create goal + strict gates
    → force_rotate / clear binding
    → wake (browser sometimes invalid JSON)
    → model commits run_command to missing scripts
    → local runner fails (exit 2)
    → stderr injected into request_reasoning
    → model retries / invents workflow type "command"
    → runner: unknown step type
    → more wait_commit
    → timebox ends
    → output still missing
    → STRICT FAIL (correct)
```

並行的另一條（較早 soft run）：
```
model writes preface only (~5KB)
    → soft check PASS (wrong)
    → user rejects
    → strict gates added
```

---

## 3. 已修復 vs 未修復

| 項目 | 狀態 |
|---|---|
| 雙重 wake 狂貼 | 已修 |
| 舊 page_id | 已修 |
| 對話 binding / force_rotate | 已修大部分 |
| 新 Goal 不繼承舊 tab | 已修 |
| 嚴格 file size/content gates | 已修 |
| soft pass 誤判 | 流程上已改（報告層） |
| workflow type `command` 別名 | **剛修** |
| 缺腳本 preflight | **未修（高優先）** |
| 原子寫入 helper | **未修** |
| browser invalid JSON 自癒 | **未修完整** |
| letter-batch milestones | **未修** |
| provenance 強制 | **未修** |

---

## 4. 建議改進路線圖

### P0（下一輪編碼必做）
1. **Script preflight** in `run_command`：缺檔 → skip/fail soft with `SCRIPT_MISSING`，resume 指示先 file_edit
2. **Atomic write helper** tool 或 exec recipe
3. **Artifact regression detector**（non-empty → empty）
4. **Wake self-heal** on invalid JSON / composer missing
5. **Book job template**：letter/chapter milestones + progressive criteria

### P1
6. Console：output size live、last stderr、last commit age  
7. `force_rotate` API 可清指定 goal binding  
8. Prefer MCP file tools over third-party connectors in resume  
9. Longer default CommitWait only when output bytes rising  

### P2
10. Provenance frontmatter  
11. Official translation skill pack under `~/.agentdock/skills`  
12. Integration test：fake browser + fake model commits missing script → expect SCRIPT_MISSING not loop  

---

## 5. 對你這次「完整翻譯」目標的務實含義

在**不改模型行為契約**的前提下，ChatGPT 網頁閉環目前能做到：
- 開 session、綁定、貼 resume、偶爾寫出部分 MD

仍不可靠的是：
- 長書一次做完
- 穩定 paste
- 不靠幽靈腳本

因此短期正確策略是：
1. 驗收用嚴格門檻（已確認）  
2. 把「寫腳本」與「跑腳本」拆成強制順序  
3.  intermediate 產物可驗收（每 20 封信）  
4. 瀏覽器健康檢查通過才開始計時  

---

## 6. 本次代碼微修
- `internal/goal/workflow.go`：step type `command` 視為 `run`，避免 `unknown workflow step type: command` 再次卡死。

---

## 7. 最終判斷
| 問題 | 判定 |
|---|---|
| 為何翻譯不完整？ | 模型只 scaffold + 本地 step 失敗空轉 + 時間盒；不是 PDF 不存在 |
| 為何曾誤判通過？ | 驗收標準錯 |
| 新 session 是否可行？ | 可以（已 PASS），但不保證完整翻譯 |
| 系統是否全壞？ | 否；session/控制面已改善，**內容生產鏈**仍是瓶頸 |


## 8. P0 implemented after postmortem

### Script preflight (`internal/goal/workflow.go`)
- Before executing `run`/`command` steps, detect `python3|bash|node ... /path/script.ext`
- If script file is missing: fail with `SCRIPT_MISSING` (exit 127) + evidence code/hint
- Avoids generic "can't open file" loops when model commits run_command before file_edit

### Atomic write helper (`file_edit action=atomic_write`)
- New action on `file_edit`: whole-file rewrite via `atomicfile.Write` (temp + fsync + rename)
- Schema/spec updated; dry_run supported
- Resume prompt now tells worker to prefer `atomic_write` and create scripts before run_command

### Tests
- `TestRunCommandScriptMissingPreflight`
- `TestFileEditAtomicWriteCreatesAndOverwrites` / DryRun


## 9. Remaining P0 implemented

### Empty-file regression detection (`internal/goal/store.go`)
- After each `ApplyExecution`, scan success-criteria paths (`file_min_bytes` / `file_min_lines` / `test -s`) and evidence-tracked paths.
- If a path previously had non-empty `bytes`/`size_bytes` evidence but is now missing/empty/below min: mark goal `regressed`, set blocker/request, append `artifact_regressed` event + evidence.
- Avoids silent 0-byte overwrites being treated as normal "not yet done".

### Browser invalid JSON self-heal (`internal/chatgpt/runtime_browser.go`)
- `isRetriableBrowserErr` covers invalid JSON / protocol / session_id / fill composer failures.
- `act()` retries: soft clear page_id → full Reset+EnsureSession → OpenChatGPT+retry.
- `EnsureSession` retries without forced chrome channel after protocol failure.

### Letter/chapter milestone templates (`internal/goal/templates.go`)
- `ApplyBookJobTemplate` adds prep + N part milestones + assemble, plus progressive criteria per part and final size/content gates.
- `goal_manage create` auto-applies when milestones empty and objective looks like book/letter translation; extracts `.md`/`.pdf` path hints from objective text.
- Constraints remind atomic_write and SCRIPT_MISSING handling.

### Tests
- `TestDetectEmptyArtifactRegressions`
- `TestApplyBookJobTemplateProgressiveCriteria`
- `TestActRecoversFromInvalidJSON`
