---
name: desktop
description: macOS desktop automation as an AgentDock Skill Runtime package.
---

# macOS Desktop Skill

AgentDock 的 macOS 桌面自动化能力已经从 core 工具拆分为此 Skill Runtime 包。

## 调用入口

- `skill_manage action=run skill=desktop operation=status`：桌面权限与依赖预检。
- `operation=observe`：`action=preflight | list_apps | app_state | window_list | snapshot | snapshot_app`。
- `operation=act`：`action=focus | move | click | double_click | scroll | drag | type | set_value | secondary_action | hotkey | wait`。
- `operation=clipboard-read` / `operation=clipboard-write`：读写剪贴板。

## 迁移说明

旧 core `desktop_*` 工具使用的 `AgentDock/desktop-artifacts` 会在首次运行 Skill 时迁移到 `AgentDock/skill-data/desktop/legacy-desktop-artifacts`，新截图和验证数据写入 `AgentDock/skill-data/desktop/artifacts`。
