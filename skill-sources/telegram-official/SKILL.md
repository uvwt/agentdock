---
name: telegram-official
description: Send Dock notifications through Telegram's official Bot API using only Python standard library and api.telegram.org.
version: 0.1.3
---

# Telegram Official Bot API Skill

通过 Telegram 官方 Bot API 给 Dock 发送通知。

## 能力

- `send`：发送普通文本消息。
- `event`：发送格式化的 Dock 事件通知。
- `updates`：读取精简后的 `getUpdates` 输出，用于发现 `chat_id`。
- `health`：调用 `getMe` 验证 token 和官方 API 连通性。

## 安全

- 只访问 `https://api.telegram.org`。
- 不登录 Telegram 个人号，不读取用户私聊历史。
- 不依赖第三方 Telegram SDK、MCP server 或代理服务。
- 不接受 token 作为 action input 或 CLI 参数。
- token 只从 `TELEGRAM_BOT_TOKEN` / `TG_BOT_TOKEN` / `.env` 读取。

## 辅助脚本执行

Skill 本身只提供流程说明。确需调用包内辅助脚本时，使用 `exec_command` 直接运行当前版本脚本，不调用任何 Skill 执行入口。

当前版本目录：

```bash
AGENTDOCK_HOME="${AGENTDOCK_HOME:-$HOME/.agentdock}"
SKILL_DIR="$AGENTDOCK_HOME/skill-store/installed/telegram-official/0.1.3"
ENV_FILE="$AGENTDOCK_HOME/skill-data/telegram-official/.env"
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
| `send` | Send a text message to the configured Telegram chat. |
| `event` | Send a formatted Dock event notification. |
| `updates` | Read compact getUpdates output to discover chat ids. |
| `health` | Call getMe to verify token and official Bot API connectivity. |
