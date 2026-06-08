# Error Handling

> How errors are handled in AgentDock.

## Overview

Errors should be specific enough for operators and clients to diagnose the failing layer: validation, permission, configuration, filesystem, network, runtime, or protocol. Prefer returning errors with context over logging and continuing silently.

## Error Types

- `internal/tools.ToolError` is the client-visible error type for MCP tools. It carries `code`, `message`, `category`, `retryable`, and optional structured `details`.
- `internal/skillruntime.Error` is the Skill Runtime error type. It carries a stable code and stage for install/run diagnostics.
- `internal/commandqueue.HandlerError` is used by Nexus command adapters to distinguish retryable and non-retryable command failures.
- `internal/jsonrpc.Error` is the JSON-RPC response shape used by the MCP server.

## Error Handling Patterns

- Validate input at the boundary where it enters the system.
- Wrap internal errors with context using `%w` so callers can preserve the causal chain.
- Return `ToolError` from tool implementations when the client can act on the error category or details.
- Keep permission failures distinct from validation failures.
- Keep configuration failures distinct from runtime execution failures.
- Redact command output, environment-derived values, and tool details before returning them to clients.

## API Error Responses

- MCP tool errors are returned through the MCP tool envelope with `isError=true` and `structuredContent`.
- JSON-RPC parse and params errors use JSON-RPC error codes from `internal/jsonrpc`.
- HTTP auth failures return `401 unauthorized`; method mismatches return `405 method not allowed`.
- Skill Runtime results include both `ok` and stable error code fields so callers can inspect failures without parsing strings.

## Common Mistakes

- Returning a plain `err.Error()` to a client when the message may contain a path, token, command output, or raw payload.
- Collapsing permission, validation, and runtime failures into one generic error.
- Logging an error and then returning `nil`, which makes smoke tests pass while behavior is broken.
- Adding a new tool without tests for invalid arguments and permission/profile boundaries.
