# 动态插件

动态插件目录由 `AGENTDOCK_PLUGIN_DIR` 或启动参数决定。在 CodingMini 上实际目录通常是：

```text
$HOME/agentdock-runtime/AgentDock/plugins/<name>/plugin.json
```

## plugin.json 基本结构

```json
{
  "name": "example",
  "description": "Example plugin",
  "version": "0.1.0",
  "actions": {
    "status": {
      "description": "Check status",
      "command": "./status.sh",
      "timeout_ms": 30000,
      "output": "json",
      "input_schema": { "type": "object", "properties": {} }
    }
  },
  "secrets": ["EXAMPLE_TOKEN"],
  "metadata": {}
}
```

## 约定

- 插件目录名必须和 `name` 一致。
- `actions` 至少定义一个 action。
- 每个 action 必须有非空 `command`。
- `workdir` 不能逃逸插件目录。
- `secrets` 中列出的环境变量会在输出中脱敏。
- 健康检查类 action 统一命名为 `status`。

## 验证

```bash
plugin_list
plugin_describe plugin=<name>
plugin_call plugin=<name> action=status
```
