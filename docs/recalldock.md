# RecallDock

RecallDock 是 AgentDock 的长期经验召回层 / unified recall store。它统一承载长期 Markdown、经验卡片 cards、笔记 notes、Git 同步和 embedding 召回。

## 公开工具

AgentDock 对模型公开的 RecallDock 工具只保留 5 个：

```text
recall_bootstrap
recall_search
recall_read
recall_write
recall_maintain
```

RecallDock 只暴露 `recall_*` 公开工具；旧记忆/笔记工具名和旧 API 默认入口都不再保留。

## 工具分工

- `recall_bootstrap`：重要任务开始时加载偏好、环境、runbook 和经验索引。模型不需要选择 project；后端固定使用默认上下文。默认紧凑输出；需要正文时再用 `recall_read`。
- `recall_search`：搜索 RecallDock 内容。模型只选择 query、kind；当 kind=note 时可用 note_scope 选择 questions 或 github-learning。prefix/scope 等内部路由由后端处理，工具描述不能再引导模型传 prefix。
- `recall_read`：按 path 读取单个 Markdown、card 或 note。
- `recall_write`：统一写入和修改入口。模型必须显式选择 `target` 和 `action`，也就是先决定写入 card、note 还是 markdown，再选择 plan/create/replace/append/patch/update_fact/diff/delete 等动作；缺少 `target/action` 会直接报错。模型侧不要选择 project；项目归属由路径或已有文件处理。
- `recall_maintain`：统一维护入口，包含同步状态、列表、lint、embedding 状态和重建索引。

## recall_write target/action

```text
target=card      原子经验卡片；适合偏好、踩坑、决策、可复用操作经验；action=plan 生成计划，action=create 且 confirmed=true 写入
target=note      notes/questions 或 notes/github-learning；适合问题讨论、未定结论、学习记录；通过 note_scope 选择 questions/github-learning
target=markdown  传统 Markdown 长文档；适合稳定项目文档、runbook、总览和结构化长期事实；支持 create/replace/append/patch/update_fact/diff/delete
```

设计原则：

```text
target/action = 模型明确选择的写入目标和动作
server_info 只推荐调用 recall_bootstrap，不推荐 project 等隐藏参数
公开 recall_* schema 使用 additionalProperties=false；runtime 只接收模型侧公开字段，不保留旧参数兼容
card/note/markdown 的 target 选择由模型负责，后端只做校验、规划和安全兜底
note 暴露 note_scope/conclusion/open_questions 以支持 questions/github-learning 并保留问题沉淀质量
patch/update_fact 暴露最低必要参数；新增 fact 必须显式 append_if_missing
card 遇到 warning 时，必须先 review，再显式 allow_warnings 才能写入
path = 权威位置
confirmed = 是否真实写入；delete 必须 confirmed=true，patch/fact 未确认时只预览
```

## recall_maintain action

```text
sync_status       查看 RecallDock Git 同步状态
list              列出条目
lint              扫描污染、敏感词或指定模式
embedding_status  查看 embedding 服务状态
reindex           重建索引，可传 prefix
reindex_cards     重建 cards 索引，等价 prefix=cards
```

## 数据格式

RecallDock 不是新建一套数据格式。现有数据继续保留：

```text
profile.md
projects/**/*.md
ops/**/*.md
cards/**/*.md
notes/**/*.md
inbox/**/*.md
```

老 Markdown、cards、notes 都仍然是合法内容。迁移只收敛模型工具入口和产品命名，不批量改历史 Markdown 路径或 frontmatter。

## 环境变量

使用以下配置：

```text
AGENTDOCK_RECALL_ENDPOINT
AGENTDOCK_RECALL_TOKEN
AGENTDOCK_RECALL_TIMEOUT_MS
```

## 部署边界

当前迁移不改变实际服务端口和 embedding 模型：

```text
RecallDock 服务入口：http://127.0.0.1:18777
BGE-M3 embedding：http://127.0.0.1:18788/v1/embeddings
容器内 embedding endpoint：http://host.docker.internal:18788/v1/embeddings
```

不要使用历史误导端口 `18780`。不要为了工具名迁移重启 OrbStack 或 Docker。

## 安全写入纪律

- 不保存 token、密码、cookie、session 或明文凭据。
- 不保存临时日志、一次性状态、未验证猜测。
- 写入长期内容前先搜索/读取已有内容，优先更新现有文件。
- cards 保持原子经验；notes 保留讨论脉络；Markdown 保留长期事实和 runbook。
- 临时测试文件必须在任务结束前清理，且不要混入源码提交。

## 备份仓库命名

RecallDock 数据备份仓库目标名为 `agentdock-recall`；GitHub 远端应使用 `agentdock-recall`，不再使用历史长期记忆仓库名作为默认仓库名。
