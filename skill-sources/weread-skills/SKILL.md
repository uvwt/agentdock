---
name: weread-skills
description: 微信读书助手，可搜索书籍、查看书架、笔记、书评、阅读统计与推荐。
version: 1.0.6
---

# 微信读书 Skill

中文别名：微信读书

该 Skill 使用宿主机 `weread-skills` 能力。API Key 从 `~/.agentdock/env/skill/weread-skills.env` 或当前命令显式环境读取，不包含在 Skill 包中。

## 操作

- `status`：验证 API Key 和微信读书 Agent Gateway 可用性。
- `list-apis`：列出服务端当前支持接口。
- `search`：搜索电子书、作者、文章、书单、听书等。
- `shelf`：读取当前账号书架。
- `call`：调用原生文档声明的任意微信读书接口，业务参数放在 `params` 对象中，入口会自动平铺并附加 `skill_version`。

原始能力文档保存在 `docs/`。

## 辅助脚本执行

Skill 本身只提供流程说明。确需调用包内辅助脚本时，使用 `exec_command` 直接运行当前版本脚本，不调用任何 Skill 执行入口。

当前版本目录：

```bash
AGENTDOCK_HOME="${AGENTDOCK_HOME:-$HOME/.agentdock}"
SKILL_DIR="$AGENTDOCK_HOME/skill-store/installed/weread-skills/1.0.6"
```

调用 `exec_command` 时传入 `skill_env: "weread-skills"`，由 AgentDock 自动加载该 Skill 的独立环境；不要在命令中手工 `source` 环境文件。

调用动作（`exec_command` 同时传入 `"skill_env": "weread-skills"`）：

```bash
printf '%s' '{"skill_action":"<动作>"}' | python3 "$SKILL_DIR/run.py"
```

输入必须是 JSON 对象。写操作仍按本文档中的确认规则执行。

| 动作 | 用途 |
|---|---|
| `status` | 验证微信读书 API Key 与 Agent Gateway 是否可用。 |
| `list-apis` | 列出微信读书 Agent Gateway 当前支持的接口。 |
| `search` | 搜索微信读书内容；scope=10 电子书，0 全部，16 网文，14 听书，6 作者，12 全文，13 书单，2 公众号，4 文章。 |
| `shelf` | 读取当前微信读书账号书架。 |
| `call` | 调用微信读书原生接口；api_name 必须属于允许的微信读书接口前缀，params 会自动平铺。 |
