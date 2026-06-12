# 签名部署更新 AgentDock

## Goal

按照记忆中的 macOS 裸机签名部署流程，将 `/Users/xx/agentdock` 当前 `main` 版本构建、稳定签名、安装到运行二进制、重启 LaunchAgent，并完成服务级和真实工具级验证。

## Requirements

* 部署前读取相关记忆，并核对真实仓库、launchd、签名身份和运行进程。
* 使用记忆中的稳定签名流程，不做 ad-hoc 或未签名替换。
* 执行 `make check`、`make install-macos`、`make restart-macos`、`make smoke-macos`。
* 部署后验证签名、PID、healthz、MCP `server_info`。
* 部署完成前做真实编码工具 smoke，而不是只看 healthz。

## Acceptance Criteria

* [x] 读取相关记忆和历史部署摘要。
* [x] 确认 launchd 入口和签名身份。
* [x] `make check` 通过。
* [x] `make install-macos` 成功并稳定签名。
* [x] LaunchAgent 重启后 healthz 恢复。
* [x] `make smoke-macos` 通过。
* [x] 真实编码工具 smoke 通过。

## Verification Evidence

* Git: `main...origin/main` left/right count `0 0` before deployment.
* `make check` passed.
* `make install-macos` built `/Users/xx/agentdock/agentdock.new.20260612230200`, signed with SHA-1 `9D54442D3B0C4DE872AEE926A44B1AF990B46D19`, and installed `/Users/xx/agentdock/agentdock`.
* `codesign --verify --verbose=4 /Users/xx/agentdock/agentdock` passed.
* Installed signature includes `Identifier=com.local.agentdock` and `Authority=AgentDock Local Code Signing`.
* Runtime backup created at `/Users/xx/agentdock-runtime/backups/agentdock/agentdock.20260612230200`.
* `make restart-macos` restarted `com.uvwt.agentdock` and healthz returned `{"ok":true}`.
* `make smoke-macos` passed: healthz, dependency checks, AppleScript visibility, screenshot permission.
* Post-restart listener PID: `22070` on `127.0.0.1:18766`.
* MCP `server_info`: `mode=host`, `path_policy=host`, `sandbox_mode=none`, `tool_profile=unified`, `desktop_enabled=true`, `memory_enabled=true`, `tool_count=38`.
* Public `https://codingmini.200399.xyz/healthz` returned `{"ok":true}`.
* Coding-tool smoke on `.tmp/codetool-verify-1781276686.txt`: `read_file`, `search_text(engine=rg)`, `edit_file` dry-run/write, `apply_patch` dry-run all passed; temp file removed.

## Out of Scope

* 不改业务代码。
* 不写入长期记忆。
* 不更改 launchd/env 配置，除非部署验证发现必须修复。
