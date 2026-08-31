<div align="center">

English | [简体中文](./README.zh-CN.md)

<img src="./docs/assets/agentdock-logo.png" alt="AgentDock logo" width="128" />

# AgentDock MCP

**Give AI agents secure, controlled access to every machine you operate.**

Open ChatGPT in your browser and manage multiple computers and servers from one conversation. Write code, change configuration, run commands, and deploy in the real environment where the work belongs—without consuming a dedicated Codex coding quota.

[Documentation](https://uvwt.github.io/agentdock-docs/) · [Download](https://github.com/uvwt/agentdock/releases) · [Community](https://qun.qq.com/universal-share/share?ac=1&authKey=Rp86bSzI7vqm87KoYlKawgsPZ440Ubhyezw6Qkgcn3JISwX3zXxsXkbS5598RrY5&busi_data=eyJncm91cENvZGUiOiIxMDgxMzM3MDE5IiwidG9rZW4iOiJ0Mlg1bUU1ZWtuZzF3SHJDT3pSaGsrOURIMlNYaXBlYllOUjNLZ1BUb1hzM2lJSTZjeVNldzU0ajl0SjRVZkx2IiwidWluIjoiMzIwMjA4ODAzMiJ9&data=W28mWvuqaLf_Fwnf0CgAJXuDs6l3A78V7AoWZnizPboCpKoQMzHzZ-UlluYo47U3tmIBHK2xIgWEVEJbTiGsPQ&svctype=4&tempid=h5_group_info)

[![CI](https://github.com/uvwt/agentdock/actions/workflows/ci.yml/badge.svg)](https://github.com/uvwt/agentdock/actions/workflows/ci.yml)
[![GitHub Release](https://img.shields.io/github/v/release/uvwt/agentdock?display_name=tag&logo=github)](https://github.com/uvwt/agentdock/releases)
[![Docker Hub](https://img.shields.io/docker/pulls/agentdockio/agentdock?logo=docker&label=Docker%20Hub)](https://hub.docker.com/r/agentdockio/agentdock)
[![GHCR](https://img.shields.io/badge/GHCR-ghcr.io%2Fuvwt%2Fagentdock-2496ED?logo=docker&logoColor=white)](https://github.com/uvwt/agentdock/pkgs/container/agentdock)
[![License](https://img.shields.io/github/license/uvwt/agentdock)](./LICENSE)

</div>

<p align="center">
  <img
    src="./docs/assets/agentdock-multi-device.png"
    alt="AgentDock: operate multiple devices from one AI conversation"
    width="100%"
  />
</p>

## What is AgentDock?

AgentDock is an independent tool runtime for AI agents.

It provides unified, secure, and controlled file, command, Git, Skill, MCP, browser automation, and task execution across local computers, remote servers, and containers. Connect multiple AgentDock instances to coordinate work across devices and finish multi-machine workflows in a single conversation.

AgentDock does not provide a chat interface or perform model inference. It focuses on one responsibility:

> Let AI agents operate real environments within explicit permission boundaries and return structured, traceable, and verifiable results.

```text
              ChatGPT / Claude / Codex
                        │
                        │ MCP (multiple instances supported)
          ┌─────────────┼─────────────┐
          ▼             ▼             ▼
   ┌───────────┐ ┌───────────┐ ┌───────────┐
   │ AgentDock │ │ AgentDock │ │ AgentDock │
   │ Local Mac │ │ LAN Host  │ │ Cloud VPS │
   └─────┬─────┘ └─────┬─────┘ └─────┬─────┘
         │             │             │
         ▼             ▼             ▼
   Files · Shell · Git  Tunnels       Proxy · Deploy
```

## What can AgentDock do?

- Manage multiple computers and servers directly from ChatGPT without repeatedly switching SSH sessions
- Write code, modify projects, run tests, and operate Git in the real local or remote environment without depending on a dedicated coding-agent quota
- Manage VPS hosts, Docker services, reverse proxies, and deployment configuration
- Inspect logs, processes, ports, and actual runtime state
- Operate authenticated web pages and macOS desktop applications
- Connect multiple AgentDock instances and coordinate cross-device work in one conversation
- Extend capabilities through Skills and dynamic MCP servers
- Persist long-running task state and continue after an interruption
- Use the same tool model across macOS, Linux, Windows, and containers
- And more

## Quick start

Regular users can install AgentDock from the official package for their operating system. You do not need the source code or Go.

See [Install AgentDock](https://uvwt.github.io/agentdock-docs/docs/getting-started/install) for the complete instructions.

| Platform | Documentation |
| --- | --- |
| Docker | [Docker installation](https://uvwt.github.io/agentdock-docs/docs/getting-started/docker) |
| Linux | [Automated Linux installation](https://uvwt.github.io/agentdock-docs/docs/getting-started/linux) |
| Linux / VPS | [Manual systemd deployment](https://uvwt.github.io/agentdock-docs/docs/getting-started/vps) |
| macOS | [macOS installation](https://uvwt.github.io/agentdock-docs/docs/getting-started/macos) |
| Windows | [Graphical Windows installer](https://uvwt.github.io/agentdock-docs/docs/getting-started/windows) |

### Choose a connection option

- **Local only:** the client and AgentDock run on the same computer.
- **Temporary public address:** ChatGPT, a phone, or another remote device needs access and no domain is ready. The address may change after the Tunnel restarts.
- **Fixed domain:** a stable address for long-term use. Requires a Cloudflare-managed domain and Tunnel Token.

After installation, get the MCP URL and Bearer Token or OAuth sign-in details from the control panel or terminal, then add them to the MCP, Tools, or Connectors settings in your client. Public access must keep authentication enabled. Do not include credentials in screenshots, issues, or public conversations.

## Connect an AI client

AgentDock exposes tools over MCP Streamable HTTP. The exact client syntax varies, but a typical configuration looks like this:

```json
{
  "mcpServers": {
    "agentdock": {
      "url": "http://127.0.0.1:8765/mcp",
      "headers": {
        "Authorization": "Bearer <AGENTDOCK_AUTH_TOKEN>"
      }
    }
  }
}
```

## Core capabilities

### Files and commands

- Read and search UTF-8 text, traverse directories, and apply structured edits
- Atomic file writes, path boundaries, and private-directory protection
- Command execution with timeout and output limits
- Separate stdout, stderr, and exit status
- Long-running command sessions, PTY, observation, input, and termination
- Output truncation and sensitive-value redaction
- macOS, Linux, Windows, and WSL support

#### Forward selected host environment variables

`exec_command` intentionally starts from a small environment instead of inheriting the complete AgentDock process environment. When a host runtime needs additional variables, configure an explicit child-to-host mapping:

```bash
export AGENTDOCK_COMMAND_ENV_FROM_ENV_JSON='{"NIX_LD":"NIX_LD","NIX_LD_LIBRARY_PATH":"NIX_LD_LIBRARY_PATH"}'
```

Only mapped variables are copied. A missing host variable is skipped. Skill environment values override the mapped host value, and an explicit `exec_command.env` value overrides both.

For a systemd or OpenRC deployment, the source variables must also exist in the AgentDock service process environment. The mapping does not read a user's login shell. For example, a NixOS service using `nix-ld` should provide the current `NIX_LD` and `NIX_LD_LIBRARY_PATH` values through the service configuration together with the mapping above. Prefer declarative NixOS service configuration over snapshotting generation-specific `/nix/store` paths into a long-lived file.

### Git and GitHub

- Read repository status, diffs, and history
- Create commits, pull, and push
- Check access to GitHub repositories
- Inspect state before a change and verify the resulting diff afterward

### Skills and dynamic MCP

Official and community Skill sources live in [uvwt/agentdock-skills](https://github.com/uvwt/agentdock-skills). This repository only keeps the three bootstrap Skills that must ship with the AgentDock runtime.

- Validate, install, activate, and roll back Skill packages
- Stable, development, canary, and pinned release channels
- Isolated environment variables and runtimes for each Skill
- Register, enable, disable, refresh, and remove dynamic MCP servers
- Streamable HTTP and stdio transports
- Search tools, inspect schemas, and perform controlled calls
- Configuration isolation between MCP servers

### Native ACP

AgentDock can optionally act as a native ACP client and host a local coding-agent adapter.

- Desktop control panels provide presets for Codex, Claude, and Grok; host configuration controls whether ACP is enabled and which adapter is selected.
- Use `acp_session` to create and manage sessions, `acp_prompt` to run and observe prompts, and `acp_interaction` to answer agent permission requests.
- Optional ACP operations are available only when the connected adapter advertises the corresponding capability.
- ACP working directories follow the host process or container security boundary rather than an AgentDock filesystem allowlist.

### Browser and desktop automation

- Start, close, and clean up browser sessions
- Navigate, click, type, select, and wait
- Inspect page text, interactive elements, errors, and network responses
- Persist login state, use dedicated browser profiles, and capture screenshots
- Use system Chrome and macOS desktop automation

### Recoverable tasks

- Persist task state
- Define explicit goals, steps, and completion conditions
- Record staged checkpoints
- Track blockers and resume after interruption
- Perform final review and evidence-based completion checks
- Reuse workflow templates

### Recall and NexusDock integration

AgentDock can optionally pair with NexusDock as a multi-device aggregation entrypoint:

- Long-term project memory
- Runbooks and experience records
- Workflow templates
- Private notes
- Multi-device state coordination
- Temporary signed Artifact downloads proxied by NexusDock while the source node is online

## Runtime directories

| Path | Purpose |
| --- | --- |
| `~/AgentDock` | Default working directory for relative file operations |
| `~/.agentdock` | AgentDock state, configuration, sessions, and extension data |

## Ports

Default MCP URL for Docker, native installs, and local development:

`http://127.0.0.1:8765/mcp`

Ports are configurable. Clients must use the address defined by the actual deployment.

For public deployments, enable Bearer Token or OAuth authentication and use HTTPS. Never expose an unauthenticated MCP service to the public internet.

## Development and contribution

Run the full check before submitting code:

```bash
make check
```

GitHub Actions continuously run tests, static checks, builds, and release validation.

User documentation is maintained separately in [`uvwt/agentdock-docs`](https://github.com/uvwt/agentdock-docs). Changes to user-visible behavior, configuration, installation, or tool schemas should update the matching documentation in the same change set.

Submit bugs and feature requests through [GitHub Issues](https://github.com/uvwt/agentdock/issues).

## ♥️ Support the project

<p>If <b>AgentDock</b> helps you, please consider giving it a <b>Star</b> ⭐. Thank you for your support!</p>
<table>
<thead>
<tr>
<th align="center">WeChat</th>
<th align="center">Alipay</th>
</tr>
</thead>
<tbody><tr>
<td align="center"><img src="./docs/assets/donation/wechat-cropped.JPG" alt="WeChat donation QR code" height="200"></td>
<td align="center"><img src="./docs/assets/donation/alipay.JPG" alt="Alipay donation QR code" height="200"></td>
</tr>
</tbody>
</table>

## Related links

- [Documentation](https://uvwt.github.io/agentdock-docs/)
- [Documentation source](https://github.com/uvwt/agentdock-docs)
- [GitHub Releases](https://github.com/uvwt/agentdock/releases)
- [GitHub Container Registry](https://github.com/uvwt/agentdock/pkgs/container/agentdock)
- [Docker Hub](https://hub.docker.com/r/agentdockio/agentdock)
- [Linux Do](https://linux.do/)

## License

Apache License 2.0. See [LICENSE](./LICENSE).

## Community

[Join the QQ group (1081337019)](https://qun.qq.com/universal-share/share?ac=1&authKey=Rp86bSzI7vqm87KoYlKawgsPZ440Ubhyezw6Qkgcn3JISwX3zXxsXkbS5598RrY5&busi_data=eyJncm91cENvZGUiOiIxMDgxMzM3MDE5IiwidG9rZW4iOiJ0Mlg1bUU1ZWtuZzF3SHJDT3pSaGsrOURIMlNYaXBlYllOUjNLZ1BUb1hzM2lJSTZjeVNldzU0ajl0SjRVZkx2IiwidWluIjoiMzIwMjA4ODAzMiJ9&data=W28mWvuqaLf_Fwnf0CgAJXuDs6l3A78V7AoWZnizPboCpKoQMzHzZ-UlluYo47U3tmIBHK2xIgWEVEJbTiGsPQ&svctype=4&tempid=h5_group_info)
