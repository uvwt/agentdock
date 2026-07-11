---
name: rsshub
description: Use the local RSSHub service for status checks, route URL construction, feed fetching, feed parsing, and route probing.
version: 0.1.2
---

# RSSHub Skill

调用 DockMini 本机 RSSHub 服务，支持状态检查、路由 URL 构建、Feed 获取、解析与路由探测。

## 约束

- RSSHub 地址仅允许 localhost/127.0.0.1。
- 路由与查询参数均经过脚本校验。

## 辅助脚本执行

Skill 本身只提供流程说明。确需调用包内辅助脚本时，使用 `exec_command` 直接运行当前版本脚本，不调用任何 Skill 执行入口。

当前版本目录：

```bash
AGENTDOCK_HOME="${AGENTDOCK_HOME:-$HOME/.agentdock}"
SKILL_DIR="$AGENTDOCK_HOME/skill-store/installed/rsshub/0.1.2"
ENV_FILE="$AGENTDOCK_HOME/skill-data/rsshub/.env"
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
| `build-url` | Build a safe local RSSHub route URL from route path and query parameters without fetching it. |
| `fetch-feed` | Fetch a RSSHub route and return raw feed text truncated to max_chars. |
| `parse-feed` | Fetch and parse a RSSHub RSS/Atom route into JSON entries. |
| `probe-route` | Probe whether a RSSHub route is reachable and summarize feed title and sample entries. |
| `status` | Check local RSSHub HTTP endpoint, Docker container status, and Redis health. |
