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

截图和验证数据写入 `AGENTDOCK_HOME/skill-data/desktop/artifacts`。旧 core `desktop_*` 工具的历史 artifacts 不再由 Skill 自动迁移。
