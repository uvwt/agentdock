# Directory Structure

> How backend code is organized in AgentDock.

## Overview

AgentDock is a single Go module. The public executable lives in `cmd/agentdock`; reusable implementation details live under `internal/`. Keep package boundaries narrow and name packages by their responsibility, not by generic layers such as `utils` or `helpers`.

## Directory Layout

```text
cmd/agentdock/                 CLI and process bootstrap
internal/auth/                 Bearer and OAuth helpers
internal/commandqueue/         Nexus command queue state and execution
internal/compatenv/            Compatibility env definitions
internal/config/               Runtime config, env defaults, path policy
internal/envregistry/          Local Skill env registry
internal/httpx/                HTTP server, OAuth endpoints, artifact serving
internal/jsonrpc/              JSON-RPC request/response helpers
internal/logx/                 Project logging wrapper around slog
internal/mcp/                  MCP server, tool descriptors, schemas
internal/nexusagent/           Local Nexus agent loop and adapters
internal/nexusclient/          Nexus client, heartbeat, local state
internal/policy/               Command permission policy
internal/sandbox/              Landlock and platform sandbox integration
internal/session/              Long-running command sessions
internal/skillruntime/         Native Skill package parsing, install, execution
internal/skillstate/           Active Skill version state
internal/textutil/             Small text helpers
internal/tools/                MCP tool runtime and tool implementations
internal/workspace/            Workspace and host path resolution
docs/                          Product and operations documentation
scripts/                       Local install, restart, and smoke scripts
```

## Module Organization

- `cmd/agentdock` wires configuration, logging, runtime construction, optional Nexus agent startup, and server startup. Keep business logic out of `main.go`.
- `internal/config` owns environment variables, command-line defaults, normalization, runtime mode, sandbox mode, and path policy.
- `internal/mcp` owns MCP protocol behavior, tool descriptors, and JSON schema surfaces. When a tool changes, update registry, input schema, output schema, and tests together.
- `internal/tools` owns tool dispatch and tool implementation. Tool functions should validate input, resolve paths through `internal/workspace`, return structured results, and use `ToolError` for client-visible failures.
- `internal/skillruntime` is the native Skill Runtime. New application-specific automation belongs here as package/runtime support or as external Skill packages, not in legacy dynamic plugin paths.
- `internal/httpx` owns HTTP endpoints and must not leak auth headers, OAuth codes, or request bodies to logs.
- `internal/nexus*` packages own device control-plane integration and should keep contract DTO usage centralized.

## Naming Conventions

- Use short lowercase package names with no underscores.
- Prefer file names that describe the feature or boundary: `skill_manage.go`, `input_schema.go`, `server_test.go`.
- Keep tests beside the package they exercise with `_test.go`.
- Avoid catch-all files like `utils.go` unless the package is already narrowly scoped.
- Use constants for profile names, runtime modes, sandbox modes, and protocol-level names.

## Examples

- `internal/tools/edit_file.go` keeps validation, exact-match semantics, and diagnostic details close to the tool implementation.
- `internal/skillruntime/manifest.go` centralizes manifest parsing and validation rather than scattering manifest checks across install and execution.
- `internal/mcp/registry_test.go` verifies profile and descriptor invariants for exposed tools.
