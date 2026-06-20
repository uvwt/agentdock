# MemoryDock

MemoryDock 是 AgentDock 的长期记忆服务，AgentDock 只暴露 `memory_*` 工具。

## 常用工具

- `memory_bootstrap`：重要任务开始时加载项目上下文。
- `memory_pack`：旧兼容入口；新任务不要默认调用，等价走 `memory_bootstrap` 的紧凑策略。
- `memory_search`：补充搜索细节。
- `memory_read`：读取单个记忆文件。
- `memory_write` / `memory_patch`：写入或更新长期记忆。
- `memory_sync_status`：查看 Git 同步状态。

## 写入纪律

只写稳定、长期有效、经过验证的信息。临时排障过程和一次性错误不要写入长期记忆。
