# Replayable command output

When a remote MCP response is lost, `session_observe` with `action=status` has
already consumed the output. A completed session is also removed by `status`.
Use `action=read` to page and retry retained command output with client-owned
offsets instead.

1. Start the command using `exec_command` with `execution_mode=async`, or use
   the session returned when an auto/sync command outlives its request.
2. Call `session_observe` with `action=read` and the returned `session_id`.
   `stdout_offset` and `stderr_offset` default to zero.
3. Save each response's `stdout_next_offset` and `stderr_next_offset` after
   receiving it, then pass those values in the next read. If a response is lost,
   retry the previous offsets. Readers keep independent offsets.
4. After the process exits, continue reading until both `stdout_truncated` and
   `stderr_truncated` are false. Process completion does not imply all output
   has been retrieved.

```json
{
  "action": "read",
  "session_id": "session-...",
  "stdout_offset": 0,
  "stderr_offset": 0,
  "max_output_bytes": 65536
}
```

Offsets count raw process bytes, before JSON encoding. Each stream is bounded
independently by `max_output_bytes`; read requires at least four bytes per page
to avoid splitting UTF-8 characters. Incomplete trailing characters remain
pending while the process runs. `stdout_offset` / `stderr_offset` in the result
identify the actual start of each page. `*_missed_bytes` report an offset that
has fallen behind the retained buffer or starts inside a UTF-8 character.
`*_omitted_bytes` report bytes still pending after the page.

Output storage remains bounded to four MiB per stream. Completed sessions are
eligible for eviction after one hour, or earlier when the existing session
count limit is reached. Output is in memory and does not survive an AgentDock
restart. A retry can report a gap if new output evicts the requested bytes.

The existing `status`, `write`, and `kill` actions keep their behavior; they can
consume output or remove the session. Use `read` consistently when retryable
observation is needed. The first read may include output already returned by
`exec_command`, so applications should choose one output observation mode.
