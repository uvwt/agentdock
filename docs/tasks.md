# 可恢复任务状态

`task_manage` 是 AgentDock 内置的轻量任务状态工具，用于部署、排障、重启、跨设备操作等可能因会话中断而需要恢复的多步骤任务。简单查询、单次 Skill 调用和单条明确命令不应创建任务。

## 边界

- 任务状态只保存在当前 AgentDock 实例，不依赖 AgentDock Nexus。
- 默认目录为 `<AGENTDOCK_DIR>/tasks`；目录权限为 `0700`，任务文件权限为 `0600`。
- 工具只记录任务状态，不执行命令，也不代理其他 AgentDock 工具。
- 模型负责规划、调用真实工具、诊断和选择后续方案。

## 状态模型

任务固定经过四个阶段：

```text
check -> execute -> verify -> closeout
```

创建任务时必须提供固定目标和至少一个完成条件。后续可以补充完成条件，但不能删除、替换或降低已有条件。只有进入 `closeout` 且每个完成条件都已有真实证据时，任务才能标记为 `completed`。

任务状态包括：

- `active`：可以继续推进。
- `blocked`：存在无法绕过的明确卡点；必须同时记录失败证据。
- `completed`：完成条件全部有证据，状态不可再修改。

## 操作

`task_manage` 提供以下 action：

- `create`、`list`、`get`
- `add_condition`、`add_evidence`
- `advance`
- `record_attempt`
- `block`、`resume`
- `complete`

同一策略最多记录两次尝试。失败尝试必须提供新的诊断和证据，防止机械重复。会话中断后，新会话通过 `list` 或已知 `task_id` 调用 `get`，即可继续未完成任务。


## 固定工作流模板

模板保存在 `<AGENTDOCK_DIR>/workflows`，任务运行时只读取本地模板，不依赖 Nexus。Nexus 后续可以作为可选控制面编辑和同步模板。

模板生命周期：

```text
draft -> validated -> active -> retired
```

已发布版本不可覆盖；修改行为必须创建新版本。创建任务时会把模板 ID、版本、SHA-256、候选匹配分数、选择理由和完整模板快照写进任务，因此旧任务恢复时不会受模板后续变化影响。

`task_manage` 的模板操作：

- `template_save`：保存或更新草稿。
- `template_validate`：校验步骤 ID、阶段、依赖、替代规则和完成条件。
- `template_publish`：发布并冻结版本。
- `template_retire`：停止新任务匹配该版本。
- `template_list`、`template_get`：查看模板。
- `template_match`：根据目标、设备、任务类型返回候选、分数和匹配理由。

模板步骤支持：必做/可选、阶段、依赖、推荐命令、允许或禁止替代。模型可以补充步骤和异常处理，但不能跳过必做步骤。必做步骤只能完成或阻塞；可选步骤可用 `skip_step` 跳过并记录原因。完成步骤必须通过 `complete_step` 写入结构化证据：类型、来源、结果、摘要，以及可选的 Artifact 引用和 SHA-256。
