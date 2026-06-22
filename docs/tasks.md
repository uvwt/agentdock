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
- `phase_checkpoint`
- `record_attempt`
- `block`、`resume`
- `complete`

同一策略最多记录两次尝试。失败尝试必须提供新的诊断和证据，防止机械重复。会话中断后，新会话通过 `list` 或已知 `task_id` 调用 `get`，即可继续未完成任务。

`create`、`advance`、`complete_step`、`block`、`resume`、`complete` 等状态变更动作默认返回 `task_id` 和紧凑摘要，不返回完整任务对象，避免带模板任务把完整快照刷进上下文；需要恢复全部步骤、条件、事件和模板快照时，再显式调用 `get`。

## 固定工作流模板

模板保存在 `<AGENTDOCK_DIR>/workflows`，任务运行时只读取本地模板，不依赖 Nexus。Nexus 后续可以作为可选控制面编辑和同步模板。

模板生命周期：

```text
draft -> validated -> active -> retired
```

已发布版本不可覆盖；修改行为必须创建新版本。创建任务时会把模板 ID、版本、SHA-256、候选匹配分数、选择理由和完整模板快照写进任务，因此旧任务恢复时不会受模板后续变化影响。

`task_manage` 的模板操作：

- `template_save`：保存或更新草稿，默认返回模板摘要。
- `template_validate`：校验步骤 ID、阶段、依赖、替代规则和完成条件，默认返回模板摘要。
- `template_publish`：发布并冻结版本，默认返回模板摘要。
- `template_retire`：停止新任务匹配该版本，默认返回模板摘要。
- `template_list`：查看模板摘要列表；`template_get`：查看单个完整模板。
- `template_match`：根据目标、设备、任务类型返回候选、分数和匹配理由。

模板匹配会做轻量文本归一化：例如用户说“一个小时”也能命中包含“一小时”关键词的时间盒模板。匹配前会把同一模板 ID 的多个 active 版本收敛为最新版本，避免旧版本和新版本同时出现在候选里。项目名类关键词（如 AgentDock、Nexus、VitaPulse）只作为上下文打分，不会单独构成语义命中，避免“任何 AgentDock 任务都匹配部署模板”。有关键词或任务类型命中时，结果只返回语义候选，避免把同设备但无关的模板都列出来；只有完全没有语义候选时，才回退到设备候选。创建带模板的任务时，模板完成条件会先进入任务；调用方额外传入的完成条件只作为补充，和模板条件高度相似的内容会被跳过，避免同一个完成要求被重复编号、重复要求录入证据。

`template_match` 支持可选的任务目标向量召回。AgentDock 不内置 embedding 模型；当运行环境配置 `AGENTDOCK_TASK_EMBEDDING_ENDPOINT` 或通用 `AGENTDOCK_EMBEDDING_ENDPOINT` 时，会调用 OpenAI-compatible `/v1/embeddings` provider，把用户目标和模板标题、描述、关键词、任务类型、完成条件、步骤标题等文本做向量相似度计算。向量命中只作为混合检索的一路语义信号，仍然保留设备过滤、任务类型、关键词、弱项目名规则、priority 和最终重排；provider 未配置、超时或返回异常时，`template_match` 会自动降级为原有关键词/结构化匹配。

推荐本机复用 RecallDock 的 BGE-M3 embedding 服务，而不是在 AgentDock 内部再部署模型：

```env
AGENTDOCK_TASK_VECTOR_SEARCH=true
AGENTDOCK_TASK_EMBEDDING_ENDPOINT=http://127.0.0.1:18788/v1/embeddings
AGENTDOCK_TASK_EMBEDDING_MODEL=BAAI/bge-m3
AGENTDOCK_TASK_VECTOR_TIMEOUT_MS=10000
AGENTDOCK_TASK_VECTOR_MIN_SCORE=0.55
```

`template_match` 返回的候选 `reason` 里出现 `vector:0.xx` 时，表示该模板来自向量语义召回；输出里的 `vector_search_enabled` 表示当前 AgentDock 实例是否已经启用 embedding-backed 匹配。

模板步骤支持：必做/可选、阶段、依赖、推荐命令、允许或禁止替代。模型可以补充步骤和异常处理，但不能跳过必做步骤。必做步骤只能完成或阻塞；可选步骤可用 `skip_step` 跳过并记录原因。单步兼容接口 `complete_step` 仍可写入结构化证据，但正常多步骤任务应优先按阶段调用 `phase_checkpoint`：一次原子写入当前阶段的多个步骤完成证据、多个完成条件证据，并选择推进一个阶段或在 closeout 完成任务。失败的 checkpoint 不会留下部分状态。

`phase_checkpoint` 必须提供 `summary`，默认只返回紧凑任务摘要，不返回完整事件和模板快照。紧凑摘要会保留 `condition_refs` 和 `current_phase_steps`，用于后续 checkpoint 直接引用 `cond_01`、步骤 id 等必要索引，避免为了查 ID 再读取完整任务快照。推荐粒度是每个 `check`、`execute`、`verify`、`closeout` 里程碑一次；不要在每条命令后写任务状态。逐项 `add_evidence`、`complete_step`、`advance` 仅用于交互式恢复、单项补录或兼容旧调用方。
