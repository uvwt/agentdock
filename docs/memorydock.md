# MemoryDock 已更名为 RecallDock

MemoryDock 是旧名称。AgentDock 当前统一使用 RecallDock / `recall_*` 作为模型公开入口。

请阅读：[`docs/recalldock.md`](recalldock.md)

## 迁移边界

- 旧 `memory_*`、`memory_card_*`、`notes_*` MCP 工具名不再作为默认公开工具。
- 公开工具收敛为：`recall_bootstrap`、`recall_search`、`recall_read`、`recall_write`、`recall_maintain`。
- 旧 Markdown、cards、notes 数据格式继续保留。
- 当前服务入口仍可使用原 18777 部署；BGE-M3 仍使用 18788。
