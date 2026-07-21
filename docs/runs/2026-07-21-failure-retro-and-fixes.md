# Goal Mode Failure Retro & Fixes

- **Date**: 2026-07-21
- **Sources**: Spiritual Letters test, Bhagavad-gita chapter1 web test, acceptance challenges

## 1. Failures observed

### F1. Soft acceptance treated scaffold as success
- Smoke checks `test -s` + `grep '^#'` passed on a **5KB preface-only** file.
- True completeness vs English extract was **0.52%**; vs full refs ~**1.7%**.
- User correctly rejected.

### F2. New Goal reused old ChatGPT session
- Requested new session for Spiritual Letters goal.
- Worker still woke into `https://chatgpt.com/c/6a5ea6e5-...` from previous Gita goal.
- Detached `browser_act` could not force new chat (`missing browser session_id`).

### F3. Non-atomic rewrite emptied artifacts
- Chapter1 MD appeared ~69KB then transiently became **0 bytes** during replan/overwrite.
- In-place truncate/rewrite is unsafe for long jobs.

### F4. Local Claude substitute polluted acceptance
- Operator-side generation was mistaken for ChatGPT-web completion.
- Corrected: ChatGPT-web only for translation acceptance.

### F5. Permission dialogs stalled the model
- ChatGPT stopped on connector allow prompts (e.g. Svananda).
- Mitigated with optional auto-approve toggle (default off).

## 2. Improvements implemented

| Fix | Detail |
|---|---|
| **New-goal session isolation** | Unbound goals always `NewConversation`; do not reuse active tab / previous goal memory |
| **Goal switch isolation** | Changing `goalID` clears in-memory conversation before wake |
| **Force rotate API** | `POST /internal/runtime/chatgpt/worker` with `{"action":"force_rotate"}`; also `goal_manage chatgpt_force_rotate`; Console button「新對話」 |
| **Content-scale criteria** | `file_min_bytes:/path:N`, `file_min_lines:/path:N`, `file_not_contains:/path:needle` |
| **Atomic write guidance** | Resume prompt requires write `.tmp` then `mv`; forbid empty-then-fill |
| **Auto-approve tools toggle** | CLI / WebUI / API (default off) for Allow prompts |
| **Tests** | File-scale criteria + unbound goal opens new conversation |

## 3. How to accept book translations now

Do **not** accept on `test -s` alone. Prefer criteria like:

```text
file_min_bytes:/abs/out.md:80000
file_min_lines:/abs/out.md:500
file_not_contains:/abs/out.md:目前已完成前言
file_not_contains:/abs/out.md:後續章節將
```

Plus manual gates for quality.

## 4. Remaining gaps
1. Enforcing atomic writes in tooling (wrapper helper), not just prompt text
2. Auto checkpoint copy when validation evidence succeeds
3. Stronger letter/chapter progress % in capsule
4. Auto-approve still DOM-fragile

## 5. Verdict of this retro
- Root causes documented from real runs
- Critical isolation + acceptance gates coded and unit-tested
- Next book test should use force_rotate + file_min_bytes criteria
