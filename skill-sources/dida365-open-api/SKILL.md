---
name: dida365-open-api
description: Use the official Dida365/TickTick Open API. Use to authorize Dida365 or TickTick via OAuth, manage projects and tasks, query completed/filtered tasks, and access official focus and habit endpoints without using private reverse-engineered APIs.
version: 0.1.3
---

# Dida365 Open API

Integration guide for the official Dida365/TickTick Open API.

Use `region=cn` for 滴答清单 / Dida365 accounts and `region=global` for TickTick accounts. The skill stores OAuth client secrets and tokens locally under the private Skill data directory and never returns them in action output.

Prefer the named operations for common project/task workflows. Use `request` only for official `/open/v1/...` endpoints that are not covered by a named action.

Official references verified when this skill was created:

- Dida365 Open API: `https://developer.dida365.com/docs/openapi.md`
- TickTick Open API: `https://developer.ticktick.com/docs/openapi.md`
- TickTick official MCP: `https://mcp.ticktick.com`

## 辅助脚本执行

Skill 本身只提供流程说明。确需调用包内辅助脚本时，使用 `exec_command` 直接运行当前版本脚本，不调用任何 Skill 执行入口。

当前版本目录：

```bash
AGENTDOCK_HOME="${AGENTDOCK_HOME:-$HOME/.agentdock}"
SKILL_DIR="$AGENTDOCK_HOME/skill-store/installed/dida365-open-api/0.1.3"
ENV_FILE="$AGENTDOCK_HOME/skill-data/dida365-open-api/.env"
```

如存在私有环境文件，先加载：

```bash
set -a
[ ! -f "$ENV_FILE" ] || . "$ENV_FILE"
set +a
```

调用动作：

```bash
printf '%s' '{"skill_action":"<动作>"}' | python3 "$SKILL_DIR/run.py"
```

输入必须是 JSON 对象。写操作仍按本文档中的确认规则执行。

| 动作 | 用途 |
|---|---|
| `status` | Show local OAuth/config state without returning secrets. Optionally validate by listing projects. |
| `auth-url` | Create a Dida365/TickTick OAuth authorization URL and store OAuth client config locally. |
| `finish-auth` | Exchange a callback URL or authorization code for a local token. Tokens are stored locally and not returned. |
| `set-token` | Store an existing OAuth access token locally for official Open API calls. The token is not returned. |
| `list-projects` | List all user projects. |
| `get-project` | Get a project by ID. |
| `get-project-data` | Get a project with task/column data. |
| `create-project` | Create a project/list. |
| `update-project` | Update a project/list. |
| `delete-project` | Delete a project/list by ID. |
| `create-task` | Create a task in a project/list. |
| `get-task` | Get a task by project ID and task ID. |
| `update-task` | Update a task. Body should include official task fields such as id, projectId, title, priority, dates, tags, and items. |
| `complete-task` | Mark a task completed. |
| `delete-task` | Delete a task by project ID and task ID. |
| `move-task` | Move one or more tasks between projects. |
| `filter-tasks` | Filter tasks by project IDs, date range, priority, tags, and status. |
| `list-completed-tasks` | List completed tasks by project IDs and/or completed time range. |
| `list-habits` | List all habits. |
| `list-focuses` | List focus records by time range and focus type. type: Pomodoro=0, Timing=1. |
| `request` | Constrained raw request for official Dida365/TickTick Open API endpoints under /open/v1. |
