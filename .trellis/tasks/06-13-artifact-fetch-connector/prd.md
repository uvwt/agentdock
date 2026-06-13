# Artifact Fetch 与 Connector 文件桥接

## 目标

1. 保留 `artifact_send`，在 MCP 工具描述中显式声明 `artifact_send.file` 的文件参数重写路径，使 GPT 沙箱文件可通过 Connector 代理挂载后进入现有端到端加密链路。
2. 新增异步反向获取工具：`artifact_fetch_create`、`artifact_fetch_status`、`artifact_fetch_download`，让 GPT/Connector 从已注册 AgentDock 设备读取文件并返回为文件资源。
3. GPT 不注册为长期 Nexus 设备；每个 Fetch 使用一次性 X25519 接收密钥和短时下载 Token。
4. Nexus 只保存密文和状态；源设备读取、打包和加密；请求侧 AgentDock 下载、解密并输出文件资源。

## 已确认产品决策

- 源文件允许使用绝对路径。
- 不弹设备端确认；使用不可删除的核心黑名单，并允许用户追加黑名单。
- 目录默认只返回受限清单；显式 `archive=true` 时打包为 tar.gz。
- Fetch 采用异步 create/status/download 三步。
- 文件硬上限 500 MiB。
- 文件成功挂载到 GPT 沙箱后才确认删除 Nexus 密文；未确认时可重试下载。
- 使用独立 `artifact.fetch` 权限语义；源设备命令类型为 `artifact.fetch`，风险等级 high。

## 安全边界

- 源路径必须为绝对路径；通过 `filepath.EvalSymlinks` 后再次做黑名单检查，拒绝符号链接逃逸。
- 核心黑名单至少包含 SSH/GPG/Keychain、AgentDock 环境文件、Nexus 设备凭据、Artifact 私钥、浏览器凭据、系统密钥/凭据目录；配置只能追加，不能删除核心规则。
- 目录清单最多 1000 项，不跟随符号链接，默认不递归；归档拒绝符号链接和特殊文件。
- Fetch Token、临时私钥、封装密钥不得进入日志、工具文本输出或数据库明文字段；Token 只存摘要。
- Connector 文件输入/输出扩展必须是附加元数据，不破坏标准 MCP 客户端。

## 主流程

1. `artifact_fetch_create` 在请求侧 AgentDock 生成临时 X25519 密钥对并持久化私钥到本地 Fetch 状态目录。
2. 请求侧用自身设备凭据向 Nexus 创建 Fetch；Nexus 保存临时公钥、源设备与请求设备，创建 `artifact.fetch` 命令。
3. 源设备验证绝对路径与黑名单：目录且未归档则回报清单；文件或归档则生成 ADR1 密文，并用请求临时公钥封装文件密钥，流式上传 Nexus。
4. `artifact_fetch_status` 查询状态或目录清单。
5. `artifact_fetch_download` 使用请求设备凭据 + 短时 Token 下载密文，在本地解密和校验，返回本地文件路径、一次性 HTTP 下载地址和标准 MCP `resource_link`。
6. 平台成功挂载后，以 `mounted=true` 调用确认动作；Nexus 删除密文并使 Token 失效。

## 验收

- 两仓 `go test ./...`、`go vet ./...`、构建通过；Nexus 契约检查通过。
- 覆盖核心黑名单、符号链接逃逸、目录清单/归档、错误临时私钥、Token 双重鉴权、重复下载和 mounted 后删除。
- 生产部署保持固定 macOS 签名 `com.local.agentdock`。
- 真实验证 DockMini 文件经 Fetch 加密上传、请求侧解密、SHA-256 一致。
- 真实验证 `artifact_send.file` 的工具描述包含文件参数重写元数据，并再次从 GPT 沙箱发起调用。
- 提交推送两个仓库并更新 `projects/agentdock/runbooks/artifact-relay.md`。
