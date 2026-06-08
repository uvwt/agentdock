# Backend Development Guidelines

> AgentDock backend conventions for code changes, reviews, and AI-assisted development.

## Overview

AgentDock is a Go service that exposes MCP tools for local and remote agent execution. The backend is a single Go module with the executable in `cmd/agentdock` and implementation packages under `internal/`.

These guidelines document the current project conventions. Follow the existing package boundaries before adding new abstractions, and keep product-facing behavior explicit, testable, and safe by default.

## Guidelines Index

| Guide | Description | Status |
|-------|-------------|--------|
| [Directory Structure](./directory-structure.md) | Module organization and file layout | Active |
| [Database Guidelines](./database-guidelines.md) | Persistent state and storage policy | Active |
| [Error Handling](./error-handling.md) | Error propagation, tool errors, and API responses | Active |
| [Quality Guidelines](./quality-guidelines.md) | Code standards, quality gates, and forbidden patterns | Active |
| [Logging Guidelines](./logging-guidelines.md) | Structured logging, log levels, and sensitive data rules | Active |

## Pre-Development Checklist

- Read the relevant guide for the package you are touching.
- Search for existing helpers and package patterns before creating new ones.
- Keep new user-facing capabilities behind the appropriate profile, permission, runtime flag, or Skill Runtime boundary.
- Do not print or persist secrets, tokens, cookies, OAuth codes, or raw tool payloads.
- Run `make check` before considering the change complete, or record the exact failing command and reason.

## Quality Check

- Run `make check`.
- Re-read [Quality Guidelines](./quality-guidelines.md) for any change that touches tools, paths, permissions, Skill Runtime, env registry, HTTP auth, command execution, or automation.
- Re-read [Error Handling](./error-handling.md) and [Logging Guidelines](./logging-guidelines.md) when changing returned errors, diagnostics, logs, or external-service handling.
- Confirm local build/runtime artifacts remain ignored and are not included in commits.
- Confirm README, `docs/`, scripts, and Makefile describe one consistent verification path.

## Productization Principles

- README stays as the product entry point; detailed operations and engineering rules belong in `docs/` and `.trellis/spec/`.
- Local runtime artifacts, generated binaries, coverage files, and rollback copies stay out of git.
- New application-specific automation should be packaged as native Skill Runtime work, not as a new legacy dynamic plugin path.
- Tool descriptors, input schemas, output schemas, runtime dispatch, and tests must stay aligned whenever a tool is added or changed.
