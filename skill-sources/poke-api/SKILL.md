---
name: poke-api
description: Send context-rich instructions to Poke through the V2 inbound API using a securely injected API key.
version: 0.1.6
---

# Poke API Skill

通过 Poke V2 API 将消息或结构化上下文发送给你的 Poke 助手。

## 操作

### `status`
检查 Skill 版本、固定端点以及 `POKE_API_KEY` 是否已从 `~/.agentdock/env/skill/poke-api.env` 或当前命令显式环境加载。不会发起网络请求，也不会输出密钥。

### `send`
向 `https://poke.com/api/v1/inbound/api-message` 发送 JSON。

输入：

- `message`：推荐使用的自然语言指令。
- `context`：可选 JSON 对象，其字段会与 `message` 合并到顶层请求体，便于附带 URL、文件路径、事件 ID 或其他结构化信息。
- `dry_run`：只构造请求，不访问 Poke；无密钥时也可使用。
- `timeout_seconds`：请求超时，范围 1–60 秒。

`message` 与 `context` 至少提供一个。若两者都提供，显式 `message` 会作为最终请求体的 `message` 字段。

## 配置

仅通过私有 Skill 环境文件或当前命令环境配置：

```text
POKE_API_KEY
```

不要把 API Key 写进 Skill 源码、输入参数、日志、README 或 Git。该 Skill 只支持 Kitchen 创建的 V2 Key 和新端点；不调用已弃用的 `/api/v1/inbound-sms/webhook`。

## 安全边界

- API 端点固定为 `https://poke.com/api/v1/inbound/api-message`，输入不能覆盖。
- Authorization Header 和 API Key 不会出现在输出中。
- HTTP 响应会递归脱敏常见凭据字段。
- 请求体限制为 512 KiB，响应体限制为 1 MiB。
- 使用 Python 标准库，无第三方依赖。

## 辅助脚本执行

Skill 本身只提供流程说明。确需调用包内辅助脚本时，使用 `exec_command` 直接运行当前版本脚本，不调用任何 Skill 执行入口。

当前版本目录：

```bash
AGENTDOCK_HOME="${AGENTDOCK_HOME:-$HOME/.agentdock}"
SKILL_DIR="$AGENTDOCK_HOME/skill-store/installed/poke-api/0.1.6"
```

调用 `exec_command` 时传入 `skill_env: "poke-api"`，由 AgentDock 自动加载该 Skill 的独立环境；不要在命令中手工 `source` 环境文件。

调用动作（`exec_command` 同时传入 `"skill_env": "poke-api"`）：

```bash
printf '%s' '{"skill_action":"<动作>"}' | python3 "$SKILL_DIR/run.py"
```

输入必须是 JSON 对象。写操作仍按本文档中的确认规则执行。

| 动作 | 用途 |
|---|---|
| `status` | Check Poke API configuration without sending a request or revealing the API key. |
| `send` | Send a natural-language message and optional top-level structured context to Poke. |
