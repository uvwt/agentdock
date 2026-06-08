# Quality Guidelines

> Code quality standards for AgentDock backend development.

## Overview

AgentDock is a security-sensitive tool runtime. Changes should be small, explicit, testable, and consistent with existing package boundaries. Productization work should improve repeatability and clarity without hiding risk behind broad abstractions.

## Required Quality Gate

Run the full gate before completing a change:

```bash
make check
```

`make check` runs:

- `gofmt -w ./cmd ./internal`
- `go test ./...`
- `go vet ./...`
- `go build -trimpath -o ./bin/agentdock ./cmd/agentdock`

For focused iteration, run the smallest relevant package test first, then finish with `make check`.

## Forbidden Patterns

- Do not bypass `internal/workspace` path resolution for workspace or host file operations.
- Do not add user-facing tools without registry, schema, dispatch, and test updates.
- Do not expose write, command, desktop action, Git mutation, Skill mutation, or Memory mutation tools in the `read-only` profile.
- Do not add new legacy dynamic plugin paths for application-specific automation; use native Skill Runtime packages.
- Do not print or persist secrets, tokens, cookies, OAuth codes, authorization headers, or raw secret-bearing payloads.
- Do not use broad shell execution when structured Go APIs or existing helpers fit the task.
- Do not leave local binaries, coverage files, rollback copies, or temporary debugging artifacts tracked by git.

## Required Patterns

- Keep tool return values structured and bounded.
- Use existing truncation and redaction helpers for command output and external responses.
- Add or update tests when touching path policy, profiles, tool descriptors, schemas, Skill Runtime validation, env registry behavior, command execution, or desktop/browser automation.
- Prefer standard library packages over new dependencies unless the benefit is clear and documented.
- Keep README concise; place detailed runbooks in `docs/`.
- Keep macOS host-mode deployment separate from Docker deployment because desktop automation requires the host.

## Testing Requirements

- Tool/profile changes: update `internal/mcp` or `internal/tools` tests for profile exposure and schema invariants.
- Path handling changes: add tests for workspace-relative, absolute, missing parent, and escape attempts.
- Skill Runtime changes: test manifest validation, install/run paths, permission checks, and secret handling.
- HTTP/auth changes: test unauthenticated and authenticated behavior without logging secrets.
- Documentation-only changes still require at least `make check` before completion unless the environment makes it impossible.

## Code Review Checklist

- Does the change preserve tool permission boundaries?
- Does the change distinguish validation, permission, configuration, runtime, and network failures?
- Are returned errors diagnostic without leaking sensitive values?
- Are local runtime artifacts ignored and documented as local-only?
- Do README, docs, Makefile, and scripts describe the same verification path?
- Is the change small enough to review and roll back?
