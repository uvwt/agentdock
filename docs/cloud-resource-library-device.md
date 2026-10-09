# Cloud 资源库设备接口（设计草案）

状态：**开发中，尚未开放远程安装**。NexusDock Cloud 与 AgentDock 都以 `uvwt/agentdock-protocol` 的稳定版本为边界；AgentDock 永远不接受 Cloud Tenant/订阅含义。

## 可移植 Skill / Plugin 与安全边界

- Skill 仍是纯 `SKILL.md` 资源包，沿用现有本地/URL安装、摘要、文件数和 128 MiB 字节限制。Cloud 修订号不写入 Skill frontmatter。
- Plugin 仍是支持 Portable/OpenAI/Claude 的包，保留 `plugin_manage validate` / `install` / `update` 和内容绑定的 `review_token`；压缩包 64 MiB、提取后 256 MiB。Cloud 修订号不覆盖插件 manifest 原版号；同名非-local 版本不同内容仍拒绝。
- **只允许导出重新安装所需的可移植源码快照**，排除所有 Env 真实值、MCP 环境、skill-data、私钥/凭证、session、Cookie、active tasks、缓存、设备绝对路径；即便允许文件有硬编码凭证风险，也应预览内容清单与风险提示。Plugin 附属的 Skill 不独立导出/计额，需导出完整 Plugin。
- 本地检验需要负责 ZIP 路径穿越、符号链接、文件类型、文件数量、解包大小、插件命令/MCP 配置风险；Cloud 只能展示报告和摘要，不能代替设备安装器。
- 远程操作必须通过用户在 Cloud 明确确认的、绑定 node_id/操作/包摘要/短有效期的一次性挑战。**现有 `review_token` 是确定性内容标识，不是设备授权或用户确认**；安装前必须复用现有 `InstallReviewedSource` / `UpdateReviewedSource`，验证失败禁止安装。
- 不把大文件塞进 WebSocket `runtime.request`，只用其传控制与结果；压缩包走限额、鉴权的专用流式传输，支持离线失败、不后台补装；兼容未声明相应能力的旧 AgentDock，旧客户端 Runtime 仍只读。
- AgentDock 的 OSS 通用 API 不依赖 Cloud Auth、Billing 或 R2。测试需要覆盖安装包摘要失配、审核令牌不匹配、导出泄密、离线、重放、超时、重复请求、安装版本冲突、多设备权限。

实现依赖：NexusDock Cloud 仓库 `docs/resource-library.md`（配套设计，非 AgentDock 发布依赖）。功能未实现前，不得声明 Cloud 支持备份和远程安装。
