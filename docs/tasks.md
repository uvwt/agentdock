# 可恢复任务状态

`task_manage` 是 AgentDock 内置的轻量任务状态工具，用于多步骤开发、部署、排障、迁移和跨设备操作。它只保存任务状态，不代替命令、测试、部署或真实验证。

简单查询、单次 Skill 调用和一条明确命令不应创建任务。

## 标准流程

```text
workflow_template_manage action=match
-> 可选：workflow_template_manage action=get_many
-> task_manage action=create
-> task_manage action=checkpoint
-> 调用真实工具完成工作和验证
-> task_manage action=final_review
-> task_manage action=complete
```

任务状态保存在当前 AgentDock 实例：

```text
~/.agentdock/tasks
```

目录权限为 `0700`，任务文件权限为 `0600`。执行中的任务不跨设备同步；Workflow 模板由 NexusDock registry 管理。

## task_manage

当前 action：

```text
create
list
get
checkpoint
block
resume
final_review
complete
```

### create

普通任务必须提供：

- `title`
- `goal`
- `completion_conditions`，至少一条

可选提供：

- `steps`，最多 12 条，每条只包含 `id` 和 `title`
- `template_id`，使用一个 active 模板
- `source_template_ids`，记录由模型组合后的 2～3 个来源模板

普通任务示例：

```json
{
  "action": "create",
  "title": "修复 OAuth",
  "goal": "修复并验证 OAuth 流程",
  "completion_conditions": [
    "测试通过",
    "真实授权成功"
  ],
  "steps": [
    {"id": "check", "title": "检查代码"},
    {"id": "modify", "title": "修改实现"},
    {"id": "test", "title": "运行测试"},
    {"id": "verify", "title": "真实验证"}
  ]
}
```

单模板任务只传 `template_id`。服务端自动读取当前 active 版本，并记录模板 ID、版本和 hash；不再要求调用方传 `template_version`、选择理由或候选列表。

```json
{
  "action": "create",
  "title": "部署 AgentDock",
  "goal": "完成 Mac mini 部署",
  "template_id": "agentdock.deploy.macos"
}
```

多模板任务必须同时显式提供：

- `source_template_ids`
- 模型组合后的 `steps`
- 模型组合后的 `completion_conditions`

服务端不会自动拼接多个模板。缺少组合结果时，创建请求会返回 `TEMPLATE_COMPOSITION_REQUIRED`。

### checkpoint

步骤开始或完成时更新进度：

```json
{
  "action": "checkpoint",
  "task_id": "tsk_xxx",
  "step_id": "test",
  "status": "in_progress",
  "summary": "正在运行全部测试"
}
```

步骤状态只有：

```text
pending
in_progress
completed
```

状态只能向前推进。一个任务同时最多有一个 `in_progress` 步骤。`checkpoint` 会更新步骤、当前阶段、最近进展摘要和事件记录。

`list` 和生命周期 action 的摘要会返回：

- `completed_step_count`
- `step_count`
- `current_step`
- 每个步骤的 `status`
- 最近 `summary`
- `blocker`

不返回百分比，调用方可直接使用“已完成步骤数 / 总步骤数”。

### list / get

`list` 按更新时间返回紧凑摘要，可用：

```text
status=active|blocked|completed
```

`get` 返回完整任务，包括步骤、完成条件、来源模板、事件和最终审查。

### block / resume

遇到无法自动绕过的真实阻塞时：

```json
{
  "action": "block",
  "task_id": "tsk_xxx",
  "summary": "SSH 连续三次连接超时"
}
```

阻塞解除后：

```json
{
  "action": "resume",
  "task_id": "tsk_xxx",
  "summary": "网络恢复，继续部署"
}
```

普通失败、临时测试不通过或仍可继续排查的问题，不应直接标记为 blocked。

### final_review / complete

真实工作完成后先调用：

```json
{
  "action": "final_review",
  "task_id": "tsk_xxx",
  "status": "pass",
  "summary": "修改、测试和真实验证均已完成",
  "verified": [
    "go test ./... 通过",
    "OAuth 授权真实成功"
  ],
  "risks": []
}
```

规则：

- `status` 只能是 `pass` 或 `failed`。
- `pass` 必须至少有一条 `verified`。
- 所有任务步骤必须已经通过 `checkpoint` 标记为 `completed`，`final_review` 不会自动补全步骤。
- `failed` 必须至少有一条 `risks`。

最终审查通过后：

```json
{
  "action": "complete",
  "task_id": "tsk_xxx"
}
```

`complete` 不再重复接收摘要，直接使用 `final_review.summary`。完成后的任务不可再修改。

## workflow_template_manage

当前 action：

```text
save
validate
publish
retire
list
get
get_many
match
vector_index
```

### match

模板匹配参数：

- `goal`：主信号
- `device`：可选设备提示
- `type`：可选工作流类型提示

`match` 返回候选、分数和匹配理由。模型根据用户目标判断使用单模板、组合模板，还是创建普通任务。

### get_many

当多个模板同时适合当前任务时，读取 2～3 个 active 模板：

```json
{
  "action": "get_many",
  "template_ids": [
    "development.grillme-implement-commit",
    "agentdock.deploy.macos"
  ]
}
```

返回内容包括每个模板的完整步骤和完成条件，并明确返回：

```text
composition_required=true
next_required_action=...
```

模型必须结合当前用户目标：

1. 删除不相关步骤。
2. 合并重复步骤和完成条件。
3. 调整最终执行顺序。
4. 生成最终 `steps` 和 `completion_conditions`。
5. 使用 `source_template_ids` 调用 `task_manage create`。

不得把多个模板原样直接拼接，服务端也不会替模型自动合并。

## 模板结构

模板仍然是流程指导，不是执行 DSL：

```json
{
  "id": "agentdock.deploy.macos",
  "version": "1.1.0",
  "title": "macOS AgentDock 部署",
  "status": "draft",
  "completion_conditions": [
    "测试通过",
    "安装成功",
    "生产服务验证通过"
  ],
  "steps": [
    {"id": "check", "title": "检查现状", "phase": "check"},
    {"id": "build", "title": "构建并安装", "phase": "execute"},
    {"id": "verify", "title": "验证生产服务", "phase": "verify"},
    {"id": "report", "title": "提交并汇报", "phase": "closeout"}
  ]
}
```

模板步骤只保留 `id`、`title` 和 `phase`。`phase` 只能是：

```text
check
execute
verify
closeout
```

## 使用原则

- 多步骤任务先 `match`，多个模板合适时再 `get_many`。
- 任务执行过程中只在步骤开始、完成、真实阻塞和最终审查时更新状态，不要在每条命令后写 checkpoint。
- 任务工具不替代测试、构建、部署、截图、healthz、server_info 或 Git 状态验证。
- 未完成真实验证时，不要调用 `complete`。
- Connector 的工具 schema 可能不会热刷新；生产真实契约以服务端 `server_info` / `tools/list` 为准。
