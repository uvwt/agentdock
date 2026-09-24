# AgentDock Portable Plugin

AgentDock Plugin Core 的最终运行格式是 Portable Plugin。对本地目录或 ZIP，`plugin_manage` 会先自动识别 AgentDock Portable、OpenAI 和 Claude 格式；OpenAI/Claude 会在审核前由 Core 转成 canonical Portable snapshot，不需要模型预先改写。

## 自动识别

`plugin_manage(source=...)` 只接收本地目录或 ZIP，不需要额外的 format/adapter 参数：

- 根目录 `plugin.json`：按 AgentDock Portable 处理；
- `.codex-plugin/plugin.json`：按 OpenAI Plugin 处理；
- `.claude-plugin/plugin.json`：按 Claude Plugin 处理；
- OpenAI 与 Claude manifest 同时存在且没有 Portable manifest：作为歧义格式拒绝；
- Portable manifest 存在时具有最高优先级；若其内容无效，按 Portable 校验失败，不静默 fallback。

OpenAI/Claude 转换后生成的 canonical snapshot 才参与 `package_digest` 和 `review_token`。原包中无法安全映射的认证/行为字段继续作为 unsupported 阻塞；已知的说明元数据和可安全省略组件会明确出现在 warnings。

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

`$schema` 必须是：

```text
https://agent-plugins.org/schemas/1.0.0/plugin.schema.json
```

核心字段：

- `name`：必填，Plugin 稳定名称；
- `version`：建议提供 SemVer；省略时仅作为 `local` 开发版本处理；
- `description`：可选；
- `author/homepage/repository/license/keywords`：可选元数据；
- `provenance`：导入外部来源时使用；
- `extensions`：只有 Core 明确理解的扩展才可能被接受，未知非空扩展会进入 unsupported。

### provenance

存在 provenance 时，`origin` 必填：

```json
{
  "provenance": {
    "origin": "https://github.com/example/plugins",
    "revision": "0123456789abcdef0123456789abcdef01234567",
    "subdir": "plugins/example",
    "format": "openai",
    "adapted": true
  }
}
```

字段含义：

- `origin`：原始上游身份，例如仓库 URL；自动转换拿不到稳定上游地址时使用原始本地包内容的 `sha256:...` 摘要，不记录临时本地路径；
- `revision`：具体 commit、tag 对应不可变 revision 或来源摘要；
- `subdir`：原始仓库内的 Plugin 相对目录；
- `format`：原始格式标签，仅用于 provenance，不改变 Core 解析行为；
- `adapted`：内容是否经过导入转换。

不要把临时目录、临时 ZIP 路径或 secret 写进 provenance。

对于 HTTP/HTTPS `origin`，使用稳定、无凭据的来源 URL：
- 不要包含 `user:password@host` 或 token/userinfo；
- 不要包含 query 参数或 fragment；
- 不要把临时签名下载 URL 当作长期 origin；应记录其稳定上游身份。

## Skills

每个 `skills/<name>/` 必须是有效 Agent Skill，且 SKILL.md 的 `name` 与目录名一致。

导入时只复制 Skill 真正需要的文件，不复制来源仓库的缓存、依赖目录和无关内容。

## MCP

根目录 `mcp.json` 使用 AgentDock Portable MCP 配置。只写 AgentDock Core 明确定义且能保持语义的 transport、URL/command、args、cwd、env/header 绑定等字段。

- Remote MCP URL 可以保留普通 query 参数；仍禁止 URL userinfo 和 fragment，凭据应放在 env-backed header 中。
- `${ENV_NAME}` 表示必填绑定；`${ENV_NAME:-}` 表示可选绑定。可选绑定在对应环境变量未配置或为空时展开为空字符串；header/env 字段本身仍会保留。
- 如果外部 MCP 配置带有 AgentDock 无法等价表达的认证或 transport 语义，不要静默丢弃后继续安装；在导入报告中明确指出。

## Review

`plugin_manage validate` 对最终包计算 `package_digest` 并生成 `review_token`。

provenance 位于 `plugin.json` 内，因此它天然进入 package digest。修改 provenance、Skill、MCP 或任意其他包内容后，旧 review token 都不能继续用于安装。
