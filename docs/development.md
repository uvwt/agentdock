# Development

This document defines the default local development and productization checks for AgentDock.

## Quality Gate

Run the full gate before considering a change complete:

```bash
make check
```

`make check` formats, tests, vets, and builds the project:

```bash
gofmt -w ./cmd ./internal
go test ./...
go vet ./...
go build -trimpath -o ./bin/agentdock ./cmd/agentdock
```

For faster iteration, run package-level tests first, then finish with `make check`.

## Local Artifacts

The repository root may contain local runtime artifacts on macOS deployments:

- `agentdock`
- `agentdock.new`
- `agentdock.prev.*`
- `agentdock.bak*`
- `agentdock.killed*`
- `bin/`
- `coverage.out`

These files are intentionally ignored by git. The root `agentdock` binary can be the active launchd-managed host binary on a Mac mini deployment, so do not delete it as part of ordinary repository cleanup.

Release artifacts should be produced outside git-tracked source files, for example under `dist/` or by a release pipeline.

## Documentation Boundaries

- Keep `README.md` concise: product summary, quick validation, common deployment entry points, and links.
- Put operational runbooks in `docs/`.
- Put AI/developer coding rules in `.trellis/spec/`.
- Never record real tokens, cookies, OAuth codes, private endpoints, or local secret values in docs.

## Change Expectations

- Keep changes scoped to the package and behavior being modified.
- Prefer existing helpers and package patterns before adding new abstractions.
- Preserve read-only profile boundaries and permission gating for high-risk tools.
- New application-specific automation should use native Skill Runtime packages, not legacy dynamic plugin paths.
- Update tests when touching tool descriptors, schemas, path policy, permissions, Skill Runtime, env registry, command execution, HTTP auth, or desktop/browser automation.
