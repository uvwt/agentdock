---
name: qinglong
description: Operate a QingLong panel through its official open API. Use for QingLong health checks, env variables, cron tasks, task logs, and controlled panel API calls from AgentDock Skill.
version: 0.1.7
---

# QingLong Skill

Use this skill to operate a QingLong panel through `/open/...` APIs.

Default local target:

- Base URL: `http://127.0.0.1:5700`
- Data directory: `/Volumes/KIOXIA/Docker/qinglong/data`
- App credentials: read from the QingLong sqlite `Apps` table, using the `system` app.

Override with environment variables when needed:

- `QINGLONG_BASE_URL`
- `QINGLONG_CLIENT_ID`
- `QINGLONG_CLIENT_SECRET`
- `QINGLONG_DATA_DIR`
- `QINGLONG_DB_PATH`

Never print tokens, client secrets, or environment variable values unless the user explicitly asks and the action supports `include_values=true`.

Preferred operations:

- `status`: check Docker compose state, HTTP health, sqlite app credential availability, and token exchange.
- `envs`, `env_create`, `env_update`, `env_delete`, `env_enable`, `env_disable`: manage environment variables.
- `crons`, `cron_detail`, `cron_create`, `cron_update`, `cron_delete`, `cron_run`, `cron_stop`, `cron_enable`, `cron_disable`, `cron_logs`, `cron_log`: manage scheduled tasks and logs.
- `api`: call a QingLong open API path directly for advanced cases.

## 辅助脚本执行

Skill 本身只提供流程说明。确需调用包内辅助脚本时，使用 `exec_command` 直接运行当前版本脚本，不调用任何 Skill 执行入口。

当前版本目录：

```bash
AGENTDOCK_HOME="${AGENTDOCK_HOME:-$HOME/.agentdock}"
SKILL_DIR="$AGENTDOCK_HOME/skill-store/installed/qinglong/0.1.7"
```

调用 `exec_command` 时传入 `skill_env: "qinglong"`，由 AgentDock 自动加载该 Skill 的独立环境；不要在命令中手工 `source` 环境文件。

调用动作（`exec_command` 同时传入 `"skill_env": "qinglong"`）：

```bash
printf '%s' '{"skill_action":"<动作>"}' | python3 "$SKILL_DIR/run.py"
```

输入必须是 JSON 对象。写操作仍按本文档中的确认规则执行。

| 动作 | 用途 |
|---|---|
| `status` | Check QingLong local compose state, HTTP health, sqlite app credentials, and token exchange. |
| `envs` | List QingLong environment variables. Values are redacted by default. |
| `env_create` | Create one or more QingLong environment variables. |
| `env_update` | Update a QingLong environment variable. |
| `env_delete` | Delete QingLong environment variables by ids. Requires confirmed=true. |
| `env_enable` | Enable QingLong environment variables by ids. |
| `env_disable` | Disable QingLong environment variables by ids. |
| `crons` | List QingLong cron tasks. |
| `cron_detail` | Get a QingLong cron task by id. |
| `cron_create` | Create a QingLong cron task. |
| `cron_update` | Update a QingLong cron task. |
| `cron_delete` | Delete QingLong cron tasks by ids. Requires confirmed=true. |
| `cron_run` | Run QingLong cron tasks by ids. |
| `cron_stop` | Stop QingLong cron tasks by ids. |
| `cron_enable` | Enable QingLong cron tasks by ids. |
| `cron_disable` | Disable QingLong cron tasks by ids. |
| `cron_logs` | List log files for a QingLong cron task. |
| `cron_log` | Read latest/current log content for a QingLong cron task. |
| `api` | Call a QingLong open API path directly. Paths are relative to /open unless api_prefix is supplied. |
