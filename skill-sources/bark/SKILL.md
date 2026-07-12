---
name: bark
description: Send Bark-compatible notifications with Python standard library while keeping the device key in the private Skill data environment.
version: 0.1.7
---

# Bark Push Skill

把 Bark 兼容 HTTP API 封装为 AgentDock 原生 Skill，用于 Dock 通知。

## 能力

- `send`：发送普通 Bark 推送。
- `event`：发送格式化的 Dock 运行事件通知。
- `health`：验证 Bark server URL 与 device key 配置；`live: true` 会真实发送一条健康检查通知。
- `url`：生成兼容 GET 调用 URL，但输出中会隐藏 device key。

## 配置

默认连接 `https://api.day.app`。可设置 `BARK_SERVER_URL` / `BARK_BASE_URL` / `BARK_URL`，或在调用中传 `server_url` 指向自建 Bark server。

设备 key 只从 `BARK_DEVICE_KEY` / `BARK_KEY` / `.env` 读取，不接受 action input 或命令行参数。可通过 `BARK_ENV_FILE` 或调用参数 `env_file` 指定 `.env` 文件。

## 安全约束

- `server_url` 仅允许 `http` / `https`，禁止 URL 内嵌用户名和密码。
- 默认使用 `POST /push`，device key 不进入 URL。
- `url` 操作只生成 URL 不发送，并对 device key 做脱敏。
- 不依赖第三方 Python 包。

## 辅助脚本执行

Skill 本身只提供流程说明。确需调用包内辅助脚本时，使用 `exec_command` 直接运行当前版本脚本，不调用任何 Skill 执行入口。

当前版本目录：

```bash
AGENTDOCK_HOME="${AGENTDOCK_HOME:-$HOME/.agentdock}"
SKILL_DIR="$AGENTDOCK_HOME/skill-store/installed/bark/0.1.7"
```

调用 `exec_command` 时传入 `skill_env: "bark"`，由 AgentDock 自动加载该 Skill 的独立环境；不要在命令中手工 `source` 环境文件。

调用动作（`exec_command` 同时传入 `"skill_env": "bark"`）：

```bash
printf '%s' '{"skill_action":"<动作>"}' | python3 "$SKILL_DIR/run.py"
```

输入必须是 JSON 对象。写操作仍按本文档中的确认规则执行。

| 动作 | 用途 |
|---|---|
| `send` | Send a Bark push notification. |
| `event` | Send a formatted Dock event notification through Bark. |
| `health` | Verify Bark configuration and optionally perform a dry or live connectivity check. |
| `url` | Build a Bark-compatible GET URL without sending it. The device key is redacted in output. |
