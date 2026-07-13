# macOS Desktop Skill 自动化

`desktop` 是纯文档 Skill。它的核心流程使用包根目录相对命令；AgentDock 只通过 `exec_command skill=desktop` 绑定当前激活包和本次子进程环境，不提供统一 Skill 执行入口。

通用设计见 [Skill 设计与运行模型](skills.md)。

## 读取说明

```text
agentdock_context
→ 匹配 desktop
→ read_file path=skill://desktop/SKILL.md
```

## 调用方式

模型先读取当前激活版本的 `SKILL.md`，再按说明调用包内辅助脚本。无需读取 state 文件、手工拼接安装版本或 `source` 私有环境文件。

预检示例：

```text
exec_command
  skill: desktop
  cmd: python3 run.py
  stdin: {"skill_action":"status","check_screenshot":true,"check_applescript":true}
```

观察应用状态：

```text
exec_command
  skill: desktop
  cmd: python3 run.py
  stdin: {"skill_action":"observe","action":"app_state","app":"Finder"}
```

`skill=desktop` 会在未显式传入 `workdir` 时把目录设为当前激活包根目录，并只向本次命令或 session 注入 `desktop` 独立环境。显式 `workdir` 和 `env` 优先；命令结束后环境不会保留在 AgentDock 主进程或系统环境中。

桌面动作优先使用 Accessibility 元素索引，坐标操作只作为兜底。写入剪贴板、点击、输入和拖拽等产生副作用的动作，必须遵循 `SKILL.md` 中的确认和验证规则。

## 权限和数据

macOS 桌面自动化只能在裸机登录会话中工作，需要当前调用链具备屏幕录制和辅助功能权限。截图和运行产物存放在 `~/.agentdock/skill-data/desktop/artifacts`；不得自动复制、提交或公开其中内容。
