---
name: desktop
description: macOS desktop automation as an AgentDock Skill Runtime package.
---

# macOS Desktop Skill

AgentDock 的 macOS 桌面自动化能力已经从 core 工具拆分为此 Skill Runtime 包。

## 调用入口

- `skill_run skill=desktop operation=status`：桌面权限与依赖预检。
- `operation=observe`：`action=preflight | list_apps | app_state | window_list | snapshot | snapshot_app`。
- `operation=act`：`action=focus | move | click | double_click | scroll | drag | type | set_value | secondary_action | hotkey | wait`。
- `operation=clipboard-read` / `operation=clipboard-write`：读写剪贴板。

## 数据目录

截图原始文件写入 `AGENTDOCK_HOME/skill-data/desktop/artifacts`；`snapshot` / `snapshot_app` 默认同时发布到 `AGENTDOCK_HOME/public-artifacts` 并返回带过期时间的签名公网 `screenshot.url`。只有显式 `return_mode=base64`、`return_mode=data_url`、`return_mode=mcp_image` 或 `return_mode=both` 时才返回内联图片数据。旧 core `desktop_*` 工具的历史 artifacts 不再由 Skill 自动迁移。

- 动作参数不支持别名：`operation=act` 必须使用 `action` 枚举值；`secondary_action` 的辅助动作名必须放在 `ax_action`；`wait` 使用 `ms`。
