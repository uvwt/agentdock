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

旧的 `memory_*`、`memory_card_*`、`notes_*` 工具名不再作为默认公开入口。底层实现可以继续复用原有 MemoryDock API 和 helper，但模型不应再看到多套入口。

## 工具分工

- `recall_bootstrap`：重要任务开始时加载项目、偏好、环境、runbook 和经验索引。默认紧凑输出；需要正文时再用 `recall_read`。
- `recall_search`：搜索 RecallDock 内容。`kind=card` 搜经验卡片，`kind=note` 搜 notes 分区，`kind=markdown/all` 搜传统 Markdown。
- `recall_read`：按 path 读取单个 Markdown、card 或 note。
- `recall_write`：统一写入和修改入口。`kind` 表示写入机制，不表示所有内容类型。
- `recall_maintain`：统一维护入口，包含同步状态、列表、lint、embedding 状态和重建索引。

## recall_write kind

```text
card         经验卡片；confirmed=false 生成计划，confirmed=true 写入
note         notes/questions 或 notes/github-learning；confirmed=false 生成计划，confirmed=true 写入
markdown     传统 Markdown 长文档；type 只是语义标签，不硬枚举
append_note  追加 note 的兼容能力，默认不推荐
patch        修改已有 Markdown，默认 dry-run
diff         只预览 diff，永不写入
fact         更新结构化事实字段
delete       删除条目，必须 confirmed=true
```

设计原则：

```text
kind = 操作机制
type = 内容语义
path = 权威位置
confirmed = 是否真实写入
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

新配置优先使用：

```text
AGENTDOCK_RECALL_ENDPOINT
AGENTDOCK_RECALL_TOKEN
RECALLDOCK_AUTH_TOKEN
AGENTDOCK_RECALL_LOGIN_USER
AGENTDOCK_RECALL_LOGIN_VALUE
AGENTDOCK_RECALL_TIMEOUT_MS
```

为避免生产失联，当前 AgentDock 仍会读取旧变量作为 fallback：

```text
AGENTDOCK_MEMORY_ENDPOINT
AGENTDOCK_MEMORY_TOKEN
MEMORYDOCK_AUTH_TOKEN
AGENTDOCK_MEMORY_LOGIN_USER
AGENTDOCK_MEMORY_LOGIN_VALUE
AGENTDOCK_MEMORY_TIMEOUT_MS
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

RecallDock 数据备份仓库目标名为 `agentdock-recall`；当前 GitHub 远端仍是历史仓库 `agentdock-memory`，脚本通过 `AGENTDOCK_RECALL_BACKUP_REMOTE` 支持切换。仓库真实重命名后，再把默认远端改为 `agentdock-recall`。
