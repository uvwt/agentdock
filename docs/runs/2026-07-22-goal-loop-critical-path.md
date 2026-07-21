# Goal loop critical path — 2026-07-22

Implements go/no-go critical path (abandon book as north-star; keep product; fix loop).

## Landed

1. **Auto next-batch `current_request`**
   - `internal/orchestrator/next_batch.go` — `buildNextBatchRequest` / `nextMissingPart` from `file_min_bytes` gates + staged `src/batch_*.txt`
   - After verify unmet: rewrite request to next missing part (not stale batch)
   - After thrash/no-commit: thrash rewrite before re-wake

2. **Thrash → hard next-action**
   - On stagnant wait (`!committed` with activity): inject anti-`search_text` + next part paths via `RequestReasoning`

3. **Soft-handle false `decision=block`**
   - `Store.CommitTurn`: `shouldSoftRejectBlock` when parts/evidence/source on disk → rewrite to continue + `block_soft_rejected` event
   - Orch: soft-unblock up to 3× when durable progress + recoverable blocker, then next-batch request

4. **Busy re-wake never heavy CDP**
   - Orch: MCP activity gate 45s before `Wake` (no CDP while tools live)
   - `pageBusy`: selector-only (stop button / tool-action-buttons), no 12k `innerText`
   - `WaitIdle`: one-strike on first CDP probe failure

5. **Micro demo fixture**
   - `cmd/demo-create-goal-micro` — 3-letter short goal for unattended green

## Tests

- `go test ./internal/orchestrator/ ./internal/goal/ ./internal/chatgpt/` — pass
- New: `next_batch_test.go`, soft-reject store tests

## Still not done (by design)

- No Spiritual Letters r10 retest
- Micro demo not live-run (needs supervised ChatGPT + orch)
- Full book job remains abandoned as acceptance bar
