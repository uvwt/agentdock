# AgentDock Portable Plugin

AgentDock Plugin Core 的最终运行格式是 Portable Plugin。对本地目录或 ZIP，`plugin_manage` 会先自动识别 AgentDock Portable、OpenAI 和 Claude 格式；OpenAI/Claude 会在审核前由 Core 转成 canonical Portable snapshot，不需要模型预先改写。

## 自动识别

`plugin_manage(source=...)` 只接收本地目录或 ZIP，不需要额外的 format/adapter 参数：

- 根目录 `plugin.json`：按 AgentDock Portable 处理；
- `.codex-plugin/plugin.json`：按 OpenAI Plugin 处理；
- `.claude-plugin/plugin.json`：按 Claude Plugin 处理；
- OpenAI 与 Claude manifest 同时存在且没有 Portable manifest：作为歧义格式拒绝；
- Portable manifest 存在时具有最高优先级；若其内容无效，按 Portable 校验失败，不静默 fallback。

OpenAI/Claude 转换后生成的 canonical snapshot 才参与 `package_digest` 和 `review_token`。第三方未知元数据和来源文件会保留；AgentDock 无法安全解释的执行/认证语义不会被激活，并明确出现在 warnings。

## 最小目录

```text
plugin-root/
└── plugin.json
```

可选组件：

```text
plugin-root/
├── plugin.json
├── skills/
│   └── example-skill/
│       └── SKILL.md
└── mcp.json
```

## plugin.json

`$schema` 可作为作者工具提示存在，但 AgentDock 不要求固定 schema URI，也不会因为第三方使用自己的 schema 而拒绝安装。

核心字段：

- `name`：必填，Plugin 稳定名称；
- `version`：可选展示身份；AgentDock 不要求 SemVer，省略时使用 `local`；
- `description`：可选；
- 其他第三方元数据可原样存在，AgentDock 不使用时不会因为字段未知或形状不同而拒绝安装；
- `provenance`：导入外部来源时使用；

### provenance

存在 provenance 时，`origin` 必填：

```json
{
  "provenance": {
    "origin": "https://github.com/example/plugins",
    "ref": "main",
    "revision": "0123456789abcdef0123456789abcdef01234567",
    "subdir": "plugins/example"
  }
}
```

字段含义：

- `origin`：原始上游身份，例如仓库 URL；自动转换拿不到稳定上游地址时使用原始本地包内容的 `sha256:...` 摘要，不记录临时本地路径；
- `ref`：可选的可跟踪 ref，例如用户明确指定或实际确认的 branch/tag；不要猜默认分支；
- `revision`：具体 commit、tag 对应不可变 revision 或来源摘要；
- `subdir`：原始仓库内的 Plugin 相对目录；

不要把临时目录、临时 ZIP 路径或 secret 写进 provenance。

### `.agentdock-import.json`

远程导入时，模型可在准备好的 Plugin 根目录写一个临时来源 sidecar，把对话和获取阶段已经确认的来源信息交给 Core：

```json
{
  "origin": "https://github.com/example/plugins",
  "ref": "main",
  "revision": "0123456789abcdef0123456789abcdef01234567",
  "subdir": "plugins/example"
}
```

sidecar 只允许 `origin/ref/revision/subdir`。Core 会严格校验这些字段，在 AgentDock 自己的 staging snapshot 中消费并删除它，再写入最终 canonical provenance。用户只提供匿名本地目录或 ZIP 时不要创建 sidecar。

对于 HTTP/HTTPS `origin`，使用稳定、无凭据的来源 URL：
- 不要包含 `user:password@host` 或 token/userinfo；
- 不要包含 query 参数或 fragment；
- 不要把临时签名下载 URL 当作长期 origin；应记录其稳定上游身份。

## Skills

每个 `skills/<name>/` 必须是有效 Agent Skill，且 SKILL.md 的 `name` 与目录名一致。

导入时只复制 Skill 真正需要的文件，不复制来源仓库的缓存、依赖目录和无关内容。

## MCP

根目录 `mcp.json` 可包含第三方额外字段。AgentDock 只会激活自己能够完整理解并安全校验的 server；有未知运行字段、未知 transport 或不安全命令/URL/env 的 server 会保留在源配置中，但不会进入活动 MCP 组件。

- Remote MCP URL 可以保留普通 query 参数；仍禁止 URL userinfo 和 fragment，凭据应放在 env-backed header 中。
- `${ENV_NAME}` 表示必填绑定；`${ENV_NAME:-}` 表示可选绑定。可选绑定在对应环境变量未配置或为空时展开为空字符串；header/env 字段本身仍会保留。
- 如果外部 MCP 配置带有 AgentDock 无法等价表达的认证或 transport 语义，Plugin 本身仍可安装，但该 MCP 必须保持不激活并在 warnings 中明确指出。

## Review

`plugin_manage validate` 对最终包计算 `package_digest` 并生成 `review_token`。

provenance 位于 `plugin.json` 内，因此它天然进入 package digest。修改 provenance、Skill、MCP 或任意其他包内容后，旧 review token 都不能继续用于安装。
