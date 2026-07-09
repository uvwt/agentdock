# macOS desktop Skill 自动化

AgentDock 的桌面自动化能力由原生 Skill Runtime 的 `desktop` Skill 提供，不再作为 core 内置 `desktop_*` 工具暴露。

## 安装与预检

```json
{"tool":"skill_package","action":"install","source":"skill-sources/desktop","confirmed_no_env":true}
```

安装后使用：

```json
{"tool":"skill_run","skill":"desktop","operation":"status","input":{"check_screenshot":true,"check_applescript":true}}
```

## 推荐调用顺序

1. `skill_run skill=desktop operation=status`：检查 `cliclick`、`osascript`、`screencapture`、剪贴板和权限。
2. `operation=observe input={"action":"list_apps"}` 或 `input={"action":"window_list"}`：确认目标应用存在。
3. `operation=observe input={"action":"app_state","app":"目标应用"}`：获取窗口截图和 Accessibility Tree。
4. 优先使用 `operation=act` 搭配 `element_index` 操作 AX 元素；坐标操作作为兜底。
5. 坐标操作尽量传 `app`、`space=window`、`verify=true`。

## 坐标与截图

- 桌面动作使用 macOS points。
- 截图像素可能是 Retina image pixels。
- `observe` 的 `snapshot` / `snapshot_app` 会返回 `screenshot_path`；需要内联图片时传 `include_image=true`。

## 数据目录

`desktop` Skill 只写入 `AgentDock/skill-data/desktop/artifacts`。旧 core 工具数据不会再由 Skill 自动迁移；需要保留旧截图时，直接按历史路径手动查看或归档。
