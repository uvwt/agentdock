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
