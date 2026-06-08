# Logging Guidelines

> How logging is done in AgentDock.

## Overview

AgentDock uses the standard library `log/slog` through `internal/logx`. Logs are JSON written to stderr so Docker, launchd, systemd, and local shells can collect them consistently.

Do not import `log/slog` directly in feature packages unless there is a strong reason. Prefer `logx` so level setup and output format remain centralized.

## Log Levels

- `debug`: high-volume diagnostic details that are safe to expose and useful only while investigating.
- `info`: lifecycle events such as server startup, selected mode/profile, enabled optional subsystems, and successful long-running agent startup.
- `warn`: degraded behavior where AgentDock can continue, such as optional dependency or environment limitations.
- `error`: failures that stop a subsystem, abort a request, or require operator attention.

## Structured Logging

- Use stable snake_case keys: `tool_profile`, `path_policy`, `memory_enabled`, `nexus_enabled`.
- Log booleans and counts as native values, not formatted strings.
- Prefer package-owned identifiers over free-form text when logging codes or states.
- Keep log messages short and event-oriented.

## What to Log

- Process startup configuration that is safe to expose: workspace path, mode, path policy, host, port, profile, enabled optional subsystems.
- Server lifecycle and listener address.
- Nexus agent lifecycle and non-secret endpoint metadata.
- Warnings that explain why an optional subsystem is unavailable.
- Errors with enough context to identify the failing layer.

## What NOT to Log

- Authorization headers, bearer tokens, OAuth codes, cookies, API keys, passwords, or secret environment values.
- Full tool argument payloads, command output, request bodies, or external API responses unless they have been explicitly redacted and bounded.
- Local private config file contents.
- Memory contents that may contain user-private operational history.

## Common Mistakes

- Adding temporary `fmt.Println` debugging output instead of structured logs.
- Logging a raw error from a command or external service before redaction.
- Logging `ok=true` style summaries without the layer-specific context needed for diagnosis.
