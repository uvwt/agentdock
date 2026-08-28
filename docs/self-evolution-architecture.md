# AgentDock 三阶段自进化架构

> 状态：已确认设计基线，尚未代表当前版本已经实现全部能力。
>
> 2026-08-15 与 Grok ACP 多轮评审收敛。后续实现以本文为架构边界，不恢复已否决的 Conversation ingest、Evolution DB、leader 等复杂组件。

## 1. 核心定位

**AgentDock 是唯一的 Evolution Engine / 自进化主体。**

NexusDock 的职责只有两类：

1. 作为 Recall / 全局 Workflow 的中心存储与多节点共享后端。
2. 在 Stage 3 使用外部模型做低频跨历史语义补漏。

NexusDock 不拥有 Evolution lifecycle policy，不决定 maturity、support/contradict 计数、verified 或 next state。

AgentDock 也不直接配置或调用 OpenAI、xAI、Anthropic 等模型 Provider：

```text
允许：
ChatGPT / Grok / Codex
        ↓ MCP
AgentDock

不允许：
AgentDock
↓ API Key
OpenAI / xAI / Anthropic
```

Stage 1 / Stage 2 利用的是**当前正在与 AgentDock 交互、已经拥有完整会话上下文的模型**。Stage 3 的外部模型只配置在 NexusDock。

一句话定义：

> Stage 1 当前 Agent 从会话中帮 AgentDock 捕获经验；Stage 2 AgentDock 自动把成熟经验用于指导，并用执行前预声明的 learning check 与冻结后的 final_review 自动结算独立证据；Stage 3 Nexus 外部模型跨历史补漏。知识生命周期和最终裁决始终只属于 AgentDock。

---

## 2. Stage 1：会话中即时学习

当前 ChatGPT / Grok / Codex 已经拥有原始会话上下文，因此由当前 Agent 发现：

- 用户纠正；
- 明确偏好；
- 架构决策；
- 项目约束；
- 可复用运维经验；
- bug pattern；
- runbook 候选。

这些学习点通过 AgentDock 统一的模型侧工具提交：

```text
evolve
```

模型侧 intent：

```text
propose
bind
supersede
retract
```

`assess` 不是模型侧 intent。它只存在于 AgentDock `EvolutionService` 内部，由 `final_review` 后的 `ResolveBindings` 使用。

`bind` 是高级可选能力，只用于执行前预声明 learning check；普通 Task 不要求 bind。需要让一个已经 `active` 的经验继续获得独立 support 时，应在 `task_manage create` 的 `learning_checks` 中预绑定，使该目标在本 Task 执行期间保持盲测、不进入 Guidance。

### 2.1 `propose`

`propose` 不要求存在 Task。

示意：

```json
{
  "intent": "propose",
  "candidate": {
    "type": "preference|decision|constraint|runbook|bug_pattern|...",
    "statement": "...",
    "scope": "...",
    "canonical_key": "..."
  }
}
```

AgentDock 返回稳定的：

```text
evolution_id
```

模型侧不能提交或决定：

```text
maturity
verified
support_count
contradict_count
next_state
proposed_transition
```

这些字段全部属于 AgentDock deterministic policy 的输出。

### 2.2 不同知识类型不走同一验证路径

- `user preference` / `explicit decision` / `constraint`：明确用户来源本身就是强证据，不要求 Stage 2 才成立。
- `environment` / `architecture fact`：优先用代码、配置、API、真实环境直接核验。
- `empirical runbook` / `bug_pattern`：主要进入 Stage 2 的真实任务实践验证。

---

## 3. Stage 2：任务实践中的 Guidance 与 Evaluation

Stage 2 不要求模型事先记住整个历史知识库，也不要求每个 Task 手工 bind 经验。

它拆成两条完全独立的通道：

```text
          ┌─ 执行前：Guidance ── 帮模型做事
Task ─────┤
          └─ 执行后：Evaluation ─ 验证历史经验
```

### 3.1 执行前：Guidance

Task start / resume 时，AgentDock 根据：

```text
goal
project
device
scope
type
```

自动从 Recall 召回少量成熟经验。

默认规则：

- 只考虑 `active` / `verified`；
- `provisional` 不进入 guidance；
- 先做 scope / project / device / type 硬过滤，再做语义召回；
- Top-K 控制在约 3~5 条；
- AgentDock 记录本 Task 看过哪些 `evolution_id`。

当前模型因此不需要主动搜索“AgentDock 以前学过什么”。

### 3.2 Task 生命周期保持独立

模型照常执行真实任务：

```text
命令 / 文件 / Git / 浏览器 / 部署 / 测试
↓
当前模型结合目标与真实结果判断 Task 是否完成
↓
task_manage.final_review
↓
complete
```

现有 `final_review` 仍然只回答：

> 这个 Task 是否真正完成？完成条件是否有真实证据？

Task 与 Evolution 是两个不同状态机：

```text
Task pass  ≠ 自动 support
Task fail  ≠ 自动 contradict
```

Evolution 绝不阻塞 Task。即使本次没有 learning check、没有产生任何 Evolution 证据，Task 也必须可以正常 `complete`.

### 3.3 执行后：Evaluation

当前实现不允许模型在看到 Task 结果后再自由决定 `support` / `contradict`。证据关系必须在执行前通过 learning check 预声明：

```text
执行前：
bind / task_manage create.learning_checks
↓
Task 执行
↓
final_review 冻结 verified facts / risks / review_revision
↓
AgentDock ResolveBindings
↓
内部 assess
↓
support / contradict / none
```

`final_review` 保存后，AgentDock 仍可根据已经固化的：

```text
goal
completion_conditions
verified facts
risks
outcome
```

返回少量只读：

```text
evolution_candidates
review_revision
```

这些 candidate 用来帮助后续任务设计新的 learning check，不能对刚结束的 Task 进行事后投票。

`final_review(pass|failed)` 只负责选择**执行前已经声明**的 `on_success` / `on_failure` 分支，本身没有学习语义。例如：

```json
{
  "intent": "bind",
  "evolution_id": "evo_xxx",
  "task_id": "tsk_xxx",
  "learning_check": {
    "on_success": "support",
    "on_failure": "contradict"
  }
}
```

真正写入生命周期证据的是 AgentDock 内部 `assess`；模型侧工具 schema 不暴露该 intent，也不能提交 maturity、计数或 next state。

AgentDock 负责：

- learning check 必须在执行前绑定；
- final_review / review_revision 快照校验；
- evidence ref 归属校验；
- scope 校验；
- 去重与同 Task 单票；
- 证据独立性；
- 防自证；
- deterministic lifecycle policy；
- 最终状态迁移。

AgentDock 本身不从任意 Task success/failure 猜业务语义。

### 3.4 防自证硬规则

如果某个 `evolution_id` 已经出现在本 Task 的 `guidance_context`，则本 Task 对该 ID 的 `support` 必须服务器硬拒绝。

原因：

```text
“系统告诉你按 A 做”
↓
“模型按 A 做了”
↓
“任务成功”
```

不能反过来证明 A 为真，否则会形成自我强化闭环。

服务器可返回类似：

```text
rejected_self_proof
```

guided 条目的真实反例仍然可以形成 `contradict`，但关系也必须在执行前通过 learning check 预声明，并引用本 Task 冻结后的失败/反例事实。`Task fail` 本身绝不自动成为 contradiction。

#### active → verified 的独立验证路径

经验达到 `support=2` 后进入 `active`，普通相似 Task 会自然收到它作为 Guidance，因此不能再用该 Task 为它追加 support。第三份独立支持必须走显式盲测：

```text
task_manage create
+ learning_checks(evolution_id, on_success/on_failure)
↓
AgentDock 在生成 Guidance 前完成绑定
↓
凡可能产生 support 的目标 evolution_id 本 Task 全程不注入 Guidance
↓
真实执行 + final_review
↓
ResolveBindings 内部结算
↓
support=3 → verified
```

这个规则不清空 `EvolutionGuidanceSeen`，也不允许创建后再把“已经看过”的 Guidance 伪装成盲测。一旦某 ID 真正进入过本 Task 的 `guidance_context` / `EvolutionGuidanceSeen`，该 Task 永远不能 support 它。resume 时也继续根据 durable binding 屏蔽盲测目标。

### 3.5 `review_revision`

`final_review` 保存后生成稳定：

```text
review_revision
```

或等价的不可变 review id。

`evolution_candidates` 携带该 revision；`ResolveBindings` 内部 assessment 也只消费当前同一个 revision 的 evidence refs。模型不提交 `assess`。

这样可以避免：

```text
final_review A 生成 candidates
↓
review 后来被改成 B
↓
旧 candidates + 新 facts 被错误组合
```

不需要额外建设 assessment session / evaluation task / evaluation state machine。

---

## 4. Stage 3：NexusDock 外部模型低频补漏

Stage 3 启用后或从 disabled→enabled 时立即尝试一次，之后按配置 interval 低频执行；配置 wake 只重读配置，不能把下一次执行不断顺延，也不能触发重复即时执行。

Stage 3 负责 Stage 1 / Stage 2 容易漏掉的长周期模式：

- 多个 Task 重复出现相似失败；
- 跨设备长期重复问题；
- Recall 语义重复或冲突；
- Workflow 提炼候选；
- 漏掉的学习点；
- scope / tags / aliases / canonicalization 建议；
- 旧历史执行与经验之间可能存在的关系。

流程：

```text
Recall
+ 多节点 Task 已固化事实
+ Workflow
+ execution summaries
        ↓
projection / whitelist / redact / truncate
        ↓
NexusDock External Model
        ↓
candidate / relation suggestion / evidence_refs
        ↓
POST 某个在线 AgentDock /internal/runtime/evolve
        ↓
同一个 EvolutionService 重新核验
        ↓
AgentDock deterministic policy
        ↓
Nexus Recall persistence
```

### 4.1 Stage 3 外部模型没有裁决权

外部模型只能建议：

```text
candidate
relation suggestion
evidence_refs
canonical / scope / tag suggestions
```

不能提交或决定：

```text
maturity
verified
support_count
contradict_count
next_state
```

模型输出本身：

```text
promotion weight = 0
```

对未预绑定的历史 Task，不能仅凭外部模型一句“Task A 支持 X”就事后投票。只有 AgentDock 能基于结构化、可验证事实独立重新确认关系时，才允许登记 evidence；否则最多生成 `provisional` candidate。

---

## 5. Recall 与 Evolution 的边界

Recall 是知识存储；Evolution 是知识生命周期。

```text
agentdock_context
→ 自动提供 query-less compact Recall 启动索引与资料入口

recall_search / recall_read
→ 索引未覆盖时搜索，或按已知 path 精确读取已有知识

recall_write
→ 明确的人/模型 CRUD

evolve
→ 自动学习、证据、生命周期与状态迁移入口
```

自动 Evolution **不复用**模型侧：

```text
recall_write(confirmed=true)
```

`confirmed=true` 是现有模型侧写 Recall 的安全语义，不应该被自动进化流程伪装成“人工确认”。

AgentDock → Nexus 应新增专用、窄化的内部 transition 写接口。

概念请求字段：

```text
evolution/card id
operation_id
expected_revision
policy_version
evidence_refs
next_state
metadata_delta
```

其中 `next_state` 已经由 AgentDock policy 计算完成。

Nexus 内部 transition API 只负责：

- auth；
- schema validation；
- CAS revision；
- `operation_id` 幂等；
- 基础 evidence-ref 存在性 / 完整性检查；
- persistence；
- audit。

Nexus 不负责：

- maturity 决策；
- support/contradict 语义计数；
- next state 决策；
- Evolution lifecycle policy。

---

## 6. 多节点并发模型

Mini / Air / VPS 上的 AgentDock 都可以运行同一份 Evolution policy：

```text
AgentDock Mini ─┐
AgentDock Air  ─┼→ Nexus Recall
AgentDock VPS  ─┘
```

**不设 leader。**

Nexus 持久化层使用：

```text
revision / CAS
operation_id
policy_version
```

保证并发安全和幂等。

CAS 冲突时：

```text
AgentDock 读取最新 card
↓
按同一 policy 重新计算
↓
重新提交
```

`policy_version` 必须跟随 card / transition metadata，避免不同 AgentDock 版本静默采用不兼容生命周期规则。

---

## 7. 生命周期与证据原则

具体 maturity 阈值尚未定案，不能把讨论阶段的数字写死成产品契约。

确定性 policy 至少遵守：

- 模型 confidence 不算真实 evidence；
- Recall 被检索、被使用不算 support；
- derived knowledge 不能支持自己或自己的后代；
- 同一执行、同一失败的重复包装不能重复算独立证据；
- support / contradiction 都必须有可追溯 evidence refs；
- volatile fact 需要 aging / TTL；
- CAS 冲突后重读、重算、重试；
- 不同 policy 版本必须显式处理兼容/迁移。

可能存在：

```text
provisional
active
verified
quarantine
retired
```

但从多少独立 evidence 进入哪个状态，待实现阶段用测试和 ACP 评审最终确定。

---

## 8. 隐私与数据边界

任何阶段都不得保存或发送：

- hidden chain-of-thought；
- system / developer 私有提示；
- secrets / API keys / tokens / cookies；
- 默认整段原始 stdout；
- 任意文件完整正文；
- 不必要的模型原始 prompt / response。

Stage 3 进入外部模型前必须：

```text
projection
→ whitelist
→ redact
→ truncate
```

`sensitive` / `local_only` 内容禁止进入外部模型。

---

## 9. 最小工具与接口集合

### 模型侧

只新增一个核心工具：

```text
evolve
```

模型侧 intent：

```text
propose
bind
supersede
retract
```

`assess` 仅为 `EvolutionService` 内部操作，不出现在 MCP `evolve` schema。

### AgentDock Task 扩展

高级验证 Task 可在 `task_manage create` 提交最多 3 个：

```text
learning_checks
```

它们在 Guidance 生成前绑定；可能产生 support 的目标会被该 Task 持续屏蔽，不进入 `guidance_context`。

Task start / resume 可返回：

```text
guidance_context
```

`final_review` 保存后可只读返回：

```text
review_revision
evolution_candidates
```

这些扩展不能让 Evolution 阻塞 Task complete。

### AgentDock Runtime

Stage 3 通过内部 HTTP 复用同一个 EvolutionService：

```http
POST /internal/runtime/evolve
```

MCP `evolve` 与 Runtime `/internal/runtime/evolve` 必须走同一个领域服务和同一套 policy，避免两套进化逻辑漂移。

长期 Stage 3 增量扫描可考虑给 Runtime Task list 增加稳定 cursor，但不是 Stage 1 / 2 前置条件。

### NexusDock

内部持久化接口：

```text
AgentDock → Nexus evolution transition API
```

Stage 3 内部能力：

```text
ModelService
Stage-3 Analyzer
structured evidence resolver / cross-node factual read
```

不对 AgentDock 暴露通用 `/chat` 或 generic inference endpoint。

---

## 10. 明确不建设

最终方案明确不建设：

```text
Conversation ingest
Experience Inbox
generic message bus
producer registry / trust framework
full Conversation DB
Evolution DB
Evolution leader
manual human Verify gate for Recall evolution
Nexus maturity policy
generic inference endpoint
AgentDock external model provider config
```

旧的“Task `complete` 前必须 retrospective”方案也不采用。

Task 的正常闭环仍然是：

```text
signals / steps / checkpoints / blocked-recovered / final_review
→ final_review(pass)
→ complete
```

Evolution 与 Task 解耦，只在旁边学习和验证。

---

## 11. 推荐实施顺序

第一阶段：

```text
evolve
+ EvolutionService
+ Nexus transition API
```

先让 Stage 1 可用。

第二阶段：

```text
Task guidance_context
+ create-time learning_checks / bind
+ final_review review_revision
+ ResolveBindings internal assess
+ evolution_candidates
+ anti-self-proof
```

形成 Stage 2。

第三阶段：

```text
Nexus ModelService
+ Stage-3 Analyzer
+ /internal/runtime/evolve
+ evidence resolver
+ 跨节点增量读取
```

最后实现 Stage 3。

原则是先把 AgentDock 自身的进化核心做稳，再给 Nexus 增加低频外部模型辅助，不能反过来把 Nexus 变成主 Evolution Engine。
