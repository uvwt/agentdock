---
name: desktop
description: macOS desktop automation as an AgentDock Skill workflow.
version: 1.0.11
---

# macOS Desktop Skill

AgentDock 的 macOS 桌面自动化能力已经从 core 工具拆分为此 Skill 工作流。

## 调用入口

- `skill_action=observe`：`action=preflight | list_apps | app_state | window_list | snapshot | snapshot_app`。
- `skill_action=act`：`action=focus | move | click | double_click | scroll | drag | type | set_value | secondary_action | hotkey | wait`。
- `skill_action=clipboard-read` / `skill_action=clipboard-write`：读写剪贴板。

## 数据目录

截图原始文件写入 `AGENTDOCK_HOME/skill-data/desktop/artifacts`；`snapshot` / `snapshot_app` 同时发布到 `AGENTDOCK_HOME/public-artifacts`，并返回包含 `artifact_id` 的轻量 `screenshot` 引用。配置 `AGENTDOCK_SERVER_URL` 时额外返回带过期时间的签名 `screenshot.url`。需要模型查看图片时，再调用 AgentDock `view_image(artifact_id=...)`；Desktop Skill 不直接返回 Base64。历史截图已统一迁入同一私有数据目录。

- 动作参数不支持别名：`skill_action=act` 必须使用 `action` 枚举值；`secondary_action` 的辅助动作名必须放在 `ax_action`；`wait` 使用 `ms`。

## 辅助脚本执行

Skill 本身只提供流程说明。确需调用包内辅助脚本时，使用 `exec_command` 直接运行当前版本脚本，不调用任何 Skill 执行入口。

当前版本目录：

```bash
AGENTDOCK_HOME="${AGENTDOCK_HOME:-$HOME/.agentdock}"
SKILL_DIR="$AGENTDOCK_HOME/skill-store/installed/desktop/1.0.11"
```

调用 `exec_command` 时传入 `skill_env: "desktop"`，由 AgentDock 自动加载该 Skill 的独立环境；不要在命令中手工 `source` 环境文件。

调用动作（`exec_command` 同时传入 `"skill_env": "desktop"`）：

```bash
printf '%s' '{"skill_action":"<动作>"}' | python3 "$SKILL_DIR/run.py"
```

输入必须是 JSON 对象。写操作仍按本文档中的确认规则执行。

| 动作 | 用途 |
|---|---|
| `status` | Run a desktop preflight check and report desktop automation readiness. |
| `observe` | Unified macOS desktop observation action: preflight, list_apps, app_state, window_list, snapshot, or snapshot_app. |
| `act` | Unified macOS desktop action: focus, move, click, double_click, scroll, drag, type, set_value, secondary_action, hotkey, or wait. |
| `clipboard-read` | Read macOS clipboard text. |
| `clipboard-write` | Write macOS clipboard text and optionally verify by reading it back. |
