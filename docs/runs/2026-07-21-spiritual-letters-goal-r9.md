# Spiritual Letters Goal Mode r9 — in progress

- **Goal**: `goal_8cf518a6f2cb3599` (workspace `spiritual-letters-goal-r9`)
- **When**: 2026-07-21 ~16:19+ local
- **Policy**: ChatGPT web + AgentDock MCP only; final ≥80000 bytes; no preface-only

## Setup fixes before start (from r8 freeze)

1. **PostPastePermissionWait** default **35s** (was 90s)
2. **waitAndResolveToolPermission**: return immediately on first `tool_permission_auto_approved`
3. **approveToolPermission**: if post-click verify CDP errors, **trust the click**
4. Hard-reset chatgpt Chrome profile + pruned browser-state before open

## Mid-run product bugs found & fixed

### A. False commit from evidence / capsule bump
- `waitCommitHandsOff` treated **any** `CapsuleVersion` bump as `commit_turn`
- `evidence_added` / `verified` bump capsule → orch thought commit landed → re-woke → **no MCP hard-block** despite `letters_01.md` on disk
- **Fix**: commit only on `reasoning_committed` event; no-MCP hard-block only when **out_bytes==0 && evidence==0**

### B. Forever-hold after any output exists
- `if outBytes > 0 { hold forever without re-paste }` stranded book jobs after batch 1
- **Fix**: hold only when output **grew this cycle** (`outBytes > beforeOut`); on stagnant wait clear wake cooldown and re-paste

### C. Worker lease blocks model commit_turn
- Loop `AcquireLease(chatgpt-web-01)` held **30m** after paste
- Model `commit_turn` failed (`goal_manage:commit_turn!`) — lease / version conflict
- **Fix**: **ReleaseLease after successful paste**; resume prompt now lists get → acquire_lease → commit_turn

## Progress (live)

| Artifact | Bytes | Notes |
|---|---:|---|
| `parts_r9/letters_01.md` | 14329 | Letters 1–10 Traditional Chinese |
| `parts_r9/letters_02.md` | 12888 | Letters 11–16 (partial batch; title says 11–16) |
| final MD | 0 | not assembled |
| **parts total** | **~27KB** | vs r5–r8: 0 durable parts |

## Runtime signals that improved

- `tool_permission_auto_approved` without Chrome 101% spin
- MCP tools: `goal_manage`, `file_edit`, `exec_command`, `read_file`, `search_text`
- Chrome Helper CPU typically **~8–25%**, not pegged at 100%
- Lease released after paste (`lease_released` events)

## Remaining issues (observed)

1. Model often **search_text loops** after writing a part instead of `acquire_lease` + `commit_turn` + next batch
2. `commit_turn` still fragile if model omits `reasoning_lease_id` / wrong `expected_capsule_version` (prompt updated; needs more live proof)
3. Full book still far from **80000** final acceptance
4. Multiple conversation rotates on operator resume (`WEB:…` changed several times) — acceptable for recovery, not ideal for continuity

## Operator actions this run

- Cancelled r8 as superseded
- Created r9, open ChatGPT, auto_approve=true, orchestrate_start
- After false no-MCP block: resume + restart with orch fixes
- Manually released stuck `chatgpt-web-01` lease once before lease-release code landed
- Kept auto_approve on while supervising

## Next improvements if still stuck

1. Soften commit_turn when no foreign lease: allow model worker to acquire even if residual lease is expired/same-goal autopilot path
2. Detect **search_text thrash** (N identical tools, no file_edit) → orch re-paste “stop searching; write letters_03 / commit”
3. Prefer re-open **bound** conversation on resume, not new chat every operator restart

## Breakthrough after lease-release fix

- Model acquired `worker_id=chatgpt-model` (not browser `chatgpt-web-01`)
- **`reasoning_committed` succeeded** (`continue: … letters_01.md …`)
- Evidence: letters_01 re-verified (13330 B), letters_02 (12888 B, letters 11–16)
- Capsule advanced (e.g. 26); criteria satisfied count rose (4+)
- Orch message: `productive progress detected; waiting for commit_turn`
- Next request from store: write `parts_r9/letters_03.md` then commit

## Status at report write

- orch: `wait_commit` hands-off, running (multi-tick after thrash early-rewake)
- Chrome: healthy (~7–15% helpers, no 101% pin)
- Goal: `awaiting_reasoning`, durable parts ~26KB, **commit_turn path proven**
- **Not complete** — need letters_03+ … assemble ≥80000 final MD


## D. search_text thrash / false "no source" blocks

- After letters_01/02 commits, model often **search_text × dozens** (many **125s timeout fails**) instead of reading a known path
- Twice issued **`decision=block`** claiming OpenAI safety blocked `full_raw.txt` / no letter-17 source
- Operator prepared **`parts_r9/src/batch_17_22.txt`** (~9.7KB) and `/tmp/spiritual_letters_goal/src_batches/*` and set `current_request` via `request_reasoning`
- Resume alone only updates `summary`, **not** `current_request` (capsule still said "請使用者貼原文") — must use **request_reasoning**
- Product gap: model can still `decision=block` even when path exists; thrash tools count as MCP activity unless productive filter applies

### Productive-wait fix (landed mid-run)
- `isProductiveMCPSummary` + stagnant re-wake after ~4m of non-productive tools
- `trackedOutputBytes` also counts evidence URIs + `parts_r*` dirs

## Live scoreboard (latest)

| Item | Value |
|---|---|
| Goal | `goal_8cf518a6f2cb3599` |
| Parts | letters_01 13330 + letters_02 12888 + **letters_03 9257** ≈ **35.5KB** |
| letters_03 | **done** (letters 17–22 ZH) |
| Final MD | 0 |
| commit_turn | **PASS** for 01/02; 03 written, commit may still be pending when freeze hit |
| Chrome freeze | reappeared briefly at **102%** when orch re-waked into busy page (`page_stuck` WaitIdle) → orch stopped / profile killed |
| Criteria satisfied | 4+ |
| Orch | **stopped** after freeze to protect Chrome |

## E. readJSONField body consume bug

- `request_reasoning` with `{summary, problem}` only applied `summary` because `readJSONField` read and discarded `r.Body` once
- Sticky false `current_problem` ("安全層阻止…") kept misleading the model
- **Fix**: buffer body and restore `r.Body` with `bytes.NewReader` before second field read

## Verdict so far

r9: first durable multi-part progress + commit_turn path + allow-stop stability.
Still incomplete vs 80KB final. Residual risk: re-wake CDP while ChatGPT tab busy can still pin renderer.

## Verdict so far

r9 is the first round with **real durable book progress + working commit path + stable Chrome**.
Remaining failure mode is **model behavior** (safety false-block / search thrash), not renderer pin from AgentDock CDP.
