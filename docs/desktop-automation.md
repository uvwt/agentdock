# macOS desktop 自动化

AgentDock 的 `desktop_*` 是通用 macOS 桌面自动化能力，不绑定具体应用。

## 推荐调用顺序

1. `desktop_preflight`：检查 `cliclick`、`osascript`、`screencapture`、剪贴板和权限。
2. `desktop_window_list` 或 `desktop_list_apps`：确认目标应用存在。
3. `desktop_get_app_state`：获取窗口截图和 Accessibility Tree。
4. 优先使用 `element_index` 操作 AX 元素；坐标操作作为兜底。
5. 坐标操作尽量传 `app`、`space=window`、`verify=true`。

## 坐标规则

- 桌面动作使用 macOS points。
- 截图像素可能是 Retina image pixels。
- `desktop_snapshot_app` 会返回窗口元数据和坐标空间提示。

## 效果验证

`desktop_click`、`desktop_double_click`、`desktop_move`、`desktop_drag` 支持截图前后 diff：

```json
{
  "app": "Finder",
  "space": "window",
  "x": 120,
  "y": 80,
  "verify": true,
  "wait_ms": 300
}
```

`ok=true` 只表示命令执行成功，不等于 UI 一定发生变化；需要看 `effect_verified`、`effect_changed`、`diff_score` 和 `error_layer`。

## 本机冒烟测试

```bash
cd ~/agentdock
make smoke-macos
```

该脚本检查 healthz、依赖、AppleScript 可见性和截图权限。
