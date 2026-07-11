# macOS Desktop Skill 自动化

`desktop` 是纯文档 Skill，模型读取说明后通过 `exec_command` 调用包内辅助脚本。AgentDock 不提供统一 Skill 执行入口。

## 读取说明

```text
agentdock_context
→ 匹配 desktop
→ read_file path=skill://desktop/SKILL.md
```

## 调用方式

先从 Skill 索引读取当前说明和版本，再使用其中记录的安装路径。当前辅助脚本从 stdin 接收一个 JSON 对象，`skill_action` 选择顶层动作；`action` 保留给观察或桌面操作的子动作。

预检示例：

```bash
SKILL_VERSION="$(python3 -c 'import json, pathlib; print(json.loads((pathlib.Path.home() / ".agentdock/skill-store/state/desktop.json").read_text())["active_version"])')"
SKILL_DIR="$HOME/.agentdock/skill-store/installed/desktop/$SKILL_VERSION"
printf '%s' '{"skill_action":"status","check_screenshot":true,"check_applescript":true}'   | python3 "$SKILL_DIR/run.py"
```

观察应用状态：

```bash
printf '%s' '{"skill_action":"observe","action":"app_state","app":"Finder"}'   | python3 "$SKILL_DIR/run.py"
```

桌面动作优先使用 Accessibility 元素索引，坐标操作只作为兜底。写入剪贴板、点击、输入和拖拽等产生副作用的动作，必须遵循 `SKILL.md` 中的确认和验证规则。

## 权限和数据

macOS 桌面自动化只能在裸机登录会话中工作，需要当前调用链具备屏幕录制和辅助功能权限。截图和运行产物存放在 `~/.agentdock/skill-data/desktop/artifacts`；不得自动复制、提交或公开其中内容。
