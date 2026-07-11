---
name: ntfy
description: Use this skill when sending notifications via ntfy.sh or self-hosted ntfy servers.
version: 0.2.3
---

# ntfy Notifications Skill

把 [ntfy.sh](https://ntfy.sh) 开源 HTTP 通知服务封装为 AgentDock 原生 Skill，用于 Dock 通知推送。

## 能力

- `send`：向 ntfy topic 发送推送通知。支持标题、优先级、标签、点击 URL、图标和 Markdown 正文。
- `event`：发送格式化的 Dock 运行事件通知，自动拼接 severity / device / service / time 等上下文。
- `health`：验证 ntfy server URL 与 topic 配置；`live: true` 会真实发送一条健康检查通知。
- `subscribe`：轮询拉取 topic 最近的消息（JSON 格式），支持 `since` 和 `limit` 参数。

## 配置

默认连接 `https://ntfy.sh`。可设置 `NTFY_SERVER_URL` 环境变量或在调用中传 `server_url` 指向自建 ntfy server。

Topic 名称从 `NTFY_TOPIC` 环境变量或调用参数 `topic` 获取。

### 认证（可选）

- **Token 认证**：设置 `NTFY_TOKEN` 环境变量，使用 `Authorization: Bearer <token>` 头。
- **Basic Auth**：设置 `NTFY_USERNAME` 和 `NTFY_PASSWORD` 环境变量。
- 无认证时使用公开 ntfy.sh 服务（topic 公开可见）。

可通过 `NTFY_ENV_FILE` 或调用参数 `env_file` 指定 `.env` 文件。

## 安全约束

- `server_url` 仅允许 `http` / `https`，禁止 URL 内嵌用户名和密码。
- 认证凭据只从环境变量 / `.env` 读取，不接受 action input 传入。
- Token 和密码在输出中自动脱敏。
- 不依赖第三方 Python 包。

## 辅助脚本执行

Skill 本身只提供流程说明。确需调用包内辅助脚本时，使用 `exec_command` 直接运行当前版本脚本，不调用任何 Skill 执行入口。

当前版本目录：

```bash
AGENTDOCK_HOME="${AGENTDOCK_HOME:-$HOME/.agentdock}"
SKILL_DIR="$AGENTDOCK_HOME/skill-store/installed/ntfy/0.2.3"
ENV_FILE="$AGENTDOCK_HOME/skill-data/ntfy/.env"
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
| `send` | Send a push notification to an ntfy topic. Supports title, priority, tags, click URL, icon, and Markdown-formatted body. |
| `event` | Send a formatted Dock event notification through ntfy. |
| `health` | Verify ntfy configuration and optionally perform a live connectivity check. |
| `subscribe` | Poll an ntfy topic for recent messages (JSON format). Returns the latest messages without establishing a long-lived connection. |
