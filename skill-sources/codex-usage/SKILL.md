---
name: codex-usage
description: Use this skill when checking the current macOS user Codex CLI account state or Codex usage quota.
version: 0.1.6
---

# Codex Usage

读取当前 macOS 用户的 Codex CLI 登录状态，并查询 ChatGPT Codex 订阅额度。只提供手动查询，不创建或管理定时任务。

## 动作

- `status`：检查 `~/.codex/auth.json` 是否存在、访问令牌是否仍有效、是否能取得 account ID。所有凭据均脱敏。
- `query`：查询当前套餐、session/weekly 窗口、credits、spend_control、可用重置次数、额外限额、code review 限额、promo/referral 等接口字段，并返回打码后的 raw_response_redacted。

## Authentication

默认读取系统当前用户的 `~/.codex/auth.json`。由于 AgentDock 的 `HOME` 可能指向 workspace，Skill 通过系统用户数据库定位真实用户目录。可通过 `CODEX_HOME` 覆盖 Codex 配置目录。

## Security

- 不接受 token 作为输入。
- 不输出 access token、refresh token、ID token、Authorization header 或真实 account ID、真实 user ID 或真实邮箱。
- 只读认证文件，不修改 Codex 登录状态。
- 只访问 ChatGPT Codex usage 接口。

## 辅助脚本执行

Skill 本体是本说明文档。确需调用包内辅助脚本时，在 Skill 包根目录使用相对路径执行；运行宿主负责切换到包根目录并把所需变量注入当前子进程。

```bash
printf '%s' '{"skill_action":"<动作>"}' | python3 run.py
```

输入必须是 JSON 对象。写操作仍按本文档中的确认规则执行。

| 动作 | 用途 |
|---|---|
| `status` | Check local Codex CLI login state without revealing credentials. |
| `query` | Query Codex usage and expose the full redacted usage API payload plus parsed quota fields. |
