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

实现依赖：NexusDock Cloud 仓库 `docs/resource-library.md`（配套设计，非 AgentDock 发布依赖）。

## 设备端实现状态

已落地的是设备本地控制面，不是 Cloud 备份或远程安装发布：

- `POST /internal/runtime/resource-library` 只接收不超过 64 KiB 的控制 JSON。未实现 `ResourceLibraryRuntime` 的旧 Runtime 返回 `RESOURCE_LIBRARY_UNSUPPORTED`。
- `export_prepare` 只导出已安装的受管 Skill/Plugin，使用递归可移植白名单，拒绝符号链接、特殊文件和私人运行时文件名，并给出硬编码凭证风险提示。`download_grant` 仍是短时本地句柄，响应不带 ZIP。
- `export_upload` 只接受 `{action,upload_url,download_grant}`。它复用该 grant，再次核对 grant 未过期、ZIP 大小和归档摘要未变，然后用已配对 Device Token 以 `PUT`、`Content-Type: application/zip`、`Content-Length` 为实际大小，把文件流式送到 `https://{paired-endpoint}/v1/nodes/library/transfer/{ticket}`。请求结束（成功、失败或超时）后销毁 grant 和 ZIP，同一 grant 不能第二次上传。响应只返回 `size`、`archive_digest` 和 `content_digest`。
- `install_prepare` 只用已配对 Nexus 的 Device Token，通过现有出站 HTTPS 客户端下载候选 ZIP。其他 Origin、本机和私网地址拒绝。`package_digest` 是可选的预期内容摘要：Cloud 不能重算 Skill/Plugin 原生 digest。原生 validate 成功后，设备把实际 `package_digest` 绑定到一次性挑战；调用方提供了就必须匹配。审核报告和 Plugin `review_token` 仍由原生安装器产生。
- `install_commit` 仍必须精确匹配确认时的 node、操作、归档摘要、实际 `package_digest`、Plugin `review_token` 和一次性挑战，然后再调用现有 Skill 安装或 Plugin `InstallReviewedSource` / `UpdateReviewedSource`。字段不匹配不消耗挑战。

上传和下载共用同一条失败关闭规则：HTTPS、无 userinfo、主机和端口等于配对原点、路径不能穿越、解析结果必须全部是公网地址、`nexusclient` 不跟随重定向。上传路径还必须是 `/v1/nodes/library/transfer/{ticket}`，ticket 是单段可见字符。Edge 用 Device Token 校验设备身份发生在 Cloud 路由之前；ticket 的 node、tenant、kind、资源名和预期摘要绑定由 Cloud 执行，AgentDock 不解释 Tenant。

DNS 重绑定没有彻底消除。授权时 `LookupIP` 拒绝非公网地址，随后 `nexusclient` 仍按主机名拨号，现有客户端不能把连接固定到刚才解析出的地址。窗口内 DNS 若改指私网，这次校验盖不住。因此非公网解析、其他 Origin 和本机配对 endpoint 一律失败关闭，不能把公网校验当成拨号锁定。

尚未完成，不能声明 Cloud 备份或远程安装已经对用户开放：

- 本仓库没有 Cloud 的 `PUT /v1/nodes/library/transfer/{ticket}` 实现。设备只有在已配对 endpoint 真正提供该路由时才能完成上传。
- 多设备权限、Cloud 侧离线队列和发布开关不在本仓库实现。上传失败或超时后不后台重试，调用方必须重新 `export_prepare`。
- 配对 endpoint 本身是本机或私网地址时，远程下载和上传都按公网 HTTPS 规则失败关闭。控制面超时沿用该路由的 2 分钟预算，取消后立即返回。
