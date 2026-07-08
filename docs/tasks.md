# 可恢复任务状态

`task_manage` 是 AgentDock 内置的轻量任务状态工具，用于部署、排障、重启、跨设备操作等可能因会话中断而需要恢复的多步骤任务。简单查询、单次 Skill 调用和单条明确命令不应创建任务。

任务工具只记录任务生命周期、阻塞原因和最终自检，不执行命令，也不代理其他 AgentDock 工具。模型仍然负责规划、调用真实工具、诊断、验证和汇报。

## 当前分工

- `workflow_template_manage`：负责模板保存、校验、发布、退役、读取、列表和匹配。
- `task_manage`：负责可恢复任务的创建、查询、阻塞、恢复、最终自检和完成。

标准闭环：

```text
workflow_template_manage action=match
-> task_manage action=create
-> 调用真实工具完成工作
-> task_manage action=final_review
-> task_manage action=complete_after_review
```

## 存储位置与权限

任务状态保存在当前 AgentDock 实例本地，不依赖 AgentDock Nexus。

```text
~/.agentdock/tasks
```

Workflow 模板保存在：

```text
~/.agentdock/tasks/workflows
```

任务和模板目录按本地状态处理，目录权限应为 `0700`，状态文件应为 `0600`。执行中的 `tasks/` 不跨设备同步；可发布模板可以按版本化定义分发到目标设备，并在目标设备本地 validate/publish/match 后生效。

## task_manage

当前 `task_manage` 只提供以下 action：

```text
create
list
get
block
resume
final_review
complete_after_review
```

### create

创建可恢复任务。必须提供：

- `title`
- `goal`
- `completion_conditions`，至少一条

如果已经通过模板匹配选定模板，可以同时传：

- `template_id`
- `template_version`
- `selected_reason`
- `template_candidates`

创建带模板任务时，模板的 `completion_conditions` 会先进入任务，调用方额外传入的完成条件作为补充。任务会保存模板 ID、版本、hash、选择理由、候选列表和模板快照，旧任务恢复时不受模板后续变化影响。

### list / get

`list` 按更新时间返回任务摘要，可用 `status=active|blocked|completed` 过滤。

`get` 读取完整任务，适合会话恢复或需要查看模板快照、事件、条件和最终自检时使用。

### block / resume

遇到无法自动绕过的真实阻塞时使用 `block`，必须提供：

- `task_id`
- `blocker`
- `evidence`

阻塞解除后使用 `resume`，必须提供 `summary`。不要把普通失败、临时测试不通过或可继续排查的问题直接标记为 blocked。

### final_review / complete_after_review

真实工作完成后，先调用 `final_review`。

`final_review` 必须提供：

- `task_id`
- `review_status=pass|failed`
- `summary`
- `verified_facts`
- `open_risks`
- `missing_checks`

当 `review_status=pass` 时：

- `verified_facts` 至少一条。
- `missing_checks` 必须为空。
- 模板步骤只作为恢复锚点和流程线索，未完成步骤会在最终自检通过时标记为已审阅完成。
- 任务进入 `closeout` 阶段。

只有最终自检通过后，才能调用 `complete_after_review`。该动作会把任务标记为 `completed`，完成后的任务不可再修改。

## workflow_template_manage

当前 `workflow_template_manage` 提供以下 action：

```text
save
validate
publish
retire
list
get
match
```

### 模板生命周期

模板生命周期只有：

```text
draft -> active -> retired
```

`validate` 是只读校验，不产生中间状态。`publish` 会读取 draft、执行校验、自动退役同一 template id 的旧 active 版本、写入 published 版本，并删除对应 draft。已发布的同一 `id + version` 不可覆盖；修改模板语义、步骤、完成条件或匹配规则时必须 bump version。

同一 template id 在运行态最多只有一个 active 版本。`match` 只对 active 模板匹配，并按同一 template id 的最新版本收敛候选。

### 模板结构

模板是流程指导，不是执行 DSL。当前核心结构：

```json
{
  "id": "agentdock.deploy.macos",
  "version": "1.1.0",
  "title": "macOS AgentDock 部署",
  "description": "构建、安装并验证 Mac mini 上的 AgentDock。",
  "status": "draft",
  "match": {
    "keywords": ["AgentDock", "部署", "macOS"],
    "devices": ["DockMini"],
    "type": "deployment"
  },
  "completion_conditions": [
    "测试通过",
    "安装成功",
    "生产 healthz 或 server_info 验证通过"
  ],
  "steps": [
    {"id": "check", "title": "检查现状", "phase": "check"},
    {"id": "build", "title": "构建并安装", "phase": "execute"},
    {"id": "verify", "title": "验证生产服务", "phase": "verify"},
    {"id": "report", "title": "提交并汇报", "phase": "closeout"}
  ]
}
```

模板步骤只保留：

```text
id
title
phase
```

`phase` 只能是：

```text
check
execute
verify
closeout
```

不要在模板里写旧 DSL 字段，例如必做标记、依赖、推荐命令、替代策略或逐步骤证据规则。当前模板步骤用于流程锚点、恢复提示和最终自检辅助，不是强制执行引擎。

### match

模板匹配使用：

```text
workflow_template_manage action=match
```

主要参数：

- `goal`：主信号。
- `device`：可选设备提示。
- `type`：可选工作流类型提示，对应模板中的 `match.type`。

`device` 和 `type` 都是匹配提示，不是硬约束；命中时加分，不匹配时不能直接排除 active 模板。项目名类关键词只作为上下文加分，不应单独构成强语义命中。

`match` 返回候选、分数、匹配理由、推荐结论和向量索引状态。模型应根据候选和任务实际情况决定是否带模板创建任务；不要机械地把最高分候选用于所有任务。

## 向量检索配置

AgentDock 不内置 embedding 模型。需要模板语义召回时，可通过 OpenAI-compatible `/v1/embeddings` provider 配置：

```env
AGENTDOCK_TASK_VECTOR_SEARCH=true
AGENTDOCK_TASK_EMBEDDING_ENDPOINT=http://127.0.0.1:18788/v1/embeddings
AGENTDOCK_TASK_EMBEDDING_MODEL=BAAI/bge-m3
AGENTDOCK_TASK_VECTOR_TIMEOUT_MS=10000
AGENTDOCK_TASK_VECTOR_MIN_SCORE=0.55
```

模板向量按 `template_id/version/hash/model` 写入：

```text
~/.agentdock/tasks/search_index.sqlite
```

输出里的 `vector_search_enabled`、`vector_index_status`、`vector_index_items`、`embedding_model` 表示当前实例是否启用向量匹配、索引状态、当前模型下已持久化的模板向量数量和模型名。provider 未配置、超时或异常时，匹配会降级为关键词/结构化匹配。

## 使用原则

- 多步骤开发、部署、排障、数据迁移、Docker、VPS 或 Git 提交推送任务，先做 `workflow_template_manage action=match`，再按需要 `task_manage action=create`。
- 任务工具不替代真实验证。测试、构建、部署、截图、healthz、server_info、Git 状态等仍要用对应真实工具完成。
- 不要在每条命令后写任务状态；任务状态只记录可恢复边界、真实阻塞和最终自检。
- 没有完成真实验证时，不要调用 `complete_after_review`。
- 当前会话的 Connector 工具 schema 可能不会热刷新；判断生产真实工具列表时，以服务端 `server_info` / `tools/list` 为准。
