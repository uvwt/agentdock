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

上传和下载共用同一条失败关闭规则：HTTPS、无 userinfo、主机和端口等于配对原点、路径不能穿越、不跟随重定向。上传路径还必须是 `/v1/nodes/library/transfer/{ticket}`，ticket 是单段可见字符。Edge 用 Device Token 校验设备身份发生在 Cloud 路由之前；ticket 的 node、tenant、kind、资源名和预期摘要绑定由 Cloud 执行，AgentDock 不解释 Tenant。

资源包传输使用专用 HTTPS 客户端，不改变普通 `nexusclient.New`。专用客户端每次 `DialContext` 重新解析配对域名，对本次得到的每个地址执行资源库专用地址判定（公网 IP 或严格的 `198.18.0.0/15` Fake-IP）；只要存在其他非公网地址，这次连接失败关闭，不会再按域名交给系统拨号。通过判定的连接目标是这些 IP。请求的 `Host` 和 TLS SNI 仍是原来的配对域名，响应和错误不返回拨号 IP。该客户端把 `Transport.Proxy` 设为 nil，因此 `HTTP_PROXY`、`HTTPS_PROXY` 和 `ALL_PROXY` 不能把这一路转到私网代理。重定向不跟随，拨号主机或端口与配对原点不一致时直接失败，Device Token 不会被送到另一个 Host。

配对域名的 DNS 本身不能被视为身份凭据，连接始终验证原配对域名的 TLS 证书。URL 授权阶段不额外进行 DNS 查询，拨号时系统 DNS 的一次解析结果决定目标地址，避免重复查询和手动 DNS 配置。

### TUN Fake-IP 网络（零配置）

Quantumult X、Clash 等 TUN DNS 可能给已配对的公共域名返回 `198.18.0.0/15` 的 Fake-IP，这类地址并不是普通公网 IP。资源库的**专用出站 HTTPS 传输**在拨号阶段额外允许这个严格的基准测试地址段交给本地代理 TUN 接管；其他非公网地址（回环、私网、链路本地、元数据地址等）仍拒绝，混合地址结果也拒绝，TLS 主机名验证、证书链、原配对域名和端口限制均不放松。

这一例外只用于传输层系统 DNS 的结果，不能把 `198.18.*` IP 字面量设置成配对端点或下载 URL，`publicIP` 的通用行为不变。设备 Token 不跟随 HTTP 重定向，不使用系统 HTTP(S) 代理。**无需额外配置任何 DNS 服务器**，也不额外把域名查询发送给第三方公共 DNS。

DEV 场景：Mac mini 的 Quantumult X 返回 `198.18.33.154`，现在应由 TUN 继续转发并通过真实 `dev.nexusdock.co` HTTPS 证书校验。安全性并非单凭 Fake-IP 地址本身保证，而取决于同源校验、TLS 和 Cloud Device Token/传输票据绑定。

**实机验证（2026-10-10）**：Mac mini 系统 DNS 仍返回 `198.18.33.154`，Rescue LaunchAgent 已移除旧的独立 DNS 环境配置。使用同一实现的 `packageTransport(endpoint, systemLookup, packageTransferIP)` 对 `https://dev.nexusdock.co/ready` 发起真实 HTTPS 请求，Go TLS 验证通过且收到 HTTP 200。单元测试另外覆盖：显式 Fake-IP 端点仍拒绝、混合 Fake-IP/普通私网地址整次拒绝、代理环境不能劫持、TLS 不受信任证书不能绕过。此前 Skill/Plugin 云端安装和设备备份 E2E 已通过，但本次零配置改动后尚未重复完整安装/备份链路。


尚未完成，不能声明 Cloud 备份或远程安装已经对用户开放：

- 本仓库没有 Cloud 的 `PUT /v1/nodes/library/transfer/{ticket}` 实现。设备只有在已配对 endpoint 真正提供该路由时才能完成上传。
- 多设备权限、Cloud 侧离线队列和发布开关不在本仓库实现。上传失败或超时后不后台重试，调用方必须重新 `export_prepare`。
- 配对 endpoint 本身是本机或私网地址时，远程下载和上传都按公网 HTTPS 规则失败关闭。控制面超时沿用该路由的 2 分钟预算，取消后立即返回。
