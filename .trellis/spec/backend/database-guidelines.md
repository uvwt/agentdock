# Storage Guidelines

> Persistent state conventions for AgentDock.

## Overview

AgentDock does not currently use a database or ORM. Persistent local state is stored as small JSON files under the AgentDock control directory or state directories selected by configuration.

Use file-backed state only for bounded runtime metadata. Do not introduce a database dependency for small state that can remain a simple JSON document.

## Current State Stores

- `internal/commandqueue/store.go` stores Nexus command queue state.
- `internal/commandqueue/outbox.go` stores upload retry envelopes.
- `internal/envregistry/store.go` stores redacted Skill environment registry metadata and values.
- `internal/nexusclient/state.go` stores Nexus device state.
- `internal/skillstate/store.go` stores active Skill versions.

## Write Patterns

- Create parent directories explicitly and secure them where package helpers already do so.
- Write to a temporary file, set permissions, then replace the target atomically when updating durable JSON state.
- Wrap I/O errors with operation context, for example `fmt.Errorf("write device state: %w", err)`.
- Keep on-disk JSON stable and versioned when future migrations are plausible.
- Never store raw tokens, cookies, OAuth codes, or secret values unless the specific store is designed for local secret handling.

## Migrations

There is no general migration framework. If a JSON state shape changes:

- Add a version field or backward-compatible decode path.
- Add focused tests for old and new shapes.
- Keep migration logic in the package that owns the file format.
- Document operational impact in `docs/` if existing users need manual action.

## Common Mistakes

- Introducing a new state file without an owner package or tests.
- Writing files directly without atomic replacement.
- Letting workspace-relative paths escape through unvalidated joins.
- Recording local machine secrets or private endpoints in docs, tests, or long-term memory.
