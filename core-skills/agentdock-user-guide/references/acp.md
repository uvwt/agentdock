# Coding Agent（ACP）

AgentDock 可以作为 ACP Client，把 ChatGPT 或其他 MCP 客户端的编码任务交给运行 AgentDock 的那台电脑上的本地 Coding Agent。当前桌面预设支持 Codex、Claude 和 Grok Build，也支持自定义 ACP Adapter。

配置 ACP 时不要先假定用户使用哪一个 Agent。先检查真实宿主机，再决定是否需要安装 Adapter 和修改 AgentDock。

## 先发现本机已有的 Agent 与 Adapter

先确认实际运行 AgentDock Core 的设备和运行用户，再检查：

- Provider CLI：`codex`、`claude`、`grok`；
- ACP Adapter：`codex-acp`、`claude-agent-acp`；
- Codex / Claude Adapter 需要的 Node.js / npm：`node`、`npm`。

POSIX 系统可以从当前运行用户的真实 PATH 和常见安装目录检查，例如：

```bash
command -v codex claude grok codex-acp claude-agent-acp node npm || true
```

Windows 可以使用当前 AgentDock 运行用户检查：

```powershell
Get-Command codex,claude,grok,codex-acp,claude-agent-acp,node,npm -ErrorAction SilentlyContinue
```

不要把“Provider CLI 已安装”误判成“ACP 已可用”。AgentDock 当前预设实际使用：

| Agent | AgentDock 使用的 ACP 入口 |
| --- | --- |
| Codex | `codex-acp`，或 npm 包 `@agentclientprotocol/codex-acp` |
| Claude | `claude-agent-acp`，或 npm 包 `@agentclientprotocol/claude-agent-acp` |
| Grok Build | 直接执行 `grok agent stdio` |
| 自定义 | 用户提供的 ACP Adapter 绝对可执行路径和参数 |

如果 `agentdock_context` 已显示 ACP 已启用并且当前 Agent 正常，不要为了“重新配置”无条件覆盖现有选择。用户已经指定目标 Agent 时按用户选择；未指定时，只有一个可用 Provider 时可以直接使用它。存在多个可用 Provider 且当前没有有效选择时，应先让用户选择，避免擅自改变账号、配额或模型来源。

## 安装缺失的 ACP Adapter

当用户明确要求“启用 / 配置 / 安装 ACP”时，这个请求可以覆盖为所选 Agent 安装缺失 Adapter 的正常步骤。先检查现状，只补缺失部分，不重复全局安装。

Codex / Claude 的官方 ACP Adapter 通过 Node.js 运行。选择这两个 Agent 时如果 `node` / `npm` 缺失，应先使用当前平台可信的现有包管理器补齐 Node.js，再安装 Adapter；默认不要用来源不明的 `curl | sh` 安装脚本，也不要为了 npm 全局写权限直接使用 `sudo npm`。

### Codex

Codex Provider CLI 和 `codex-acp` 是两个不同组件。确认用户已经安装并登录 Codex 后，如果缺少 Adapter，可以安装官方 ACP npm 包：

```bash
npm install -g @agentclientprotocol/codex-acp
```

POSIX 上如果系统全局 npm 目录不可写，优先使用用户目录而不是 `sudo npm`：

```bash
npm install --global --prefix "$HOME/.local" @agentclientprotocol/codex-acp
```

AgentDock macOS Desktop 会搜索 `~/.local/bin`、Homebrew / local bin 和 PATH，也会识别对应 npm package 的 Node.js 入口。

### Claude

确认用户已经安装并登录 Claude Code 后，如果缺少 Adapter，可以安装：

```bash
npm install -g @agentclientprotocol/claude-agent-acp
```

POSIX 上系统全局 npm 目录不可写时同样优先用户目录：

```bash
npm install --global --prefix "$HOME/.local" @agentclientprotocol/claude-agent-acp
```

### Grok Build

Grok Build 预设不需要另装 npm ACP Adapter。AgentDock 直接启动：

```text
grok agent stdio
```

因此只需确认 `grok` 可执行文件真实存在、当前 AgentDock 运行用户可以执行，并且 Grok 自己的登录 / 授权状态已经可用。

不要在聊天、日志或配置文件中回显 Provider Token。Codex、Claude、Grok 的账号登录由对应 Provider 自己管理；AgentDock 只负责启动 Adapter。只有确实需要把宿主环境变量映射给自定义 Adapter 时，才在对应 Profile 的 `env_from_env` 中声明变量名映射，并且只保存变量名，不写入 secret 值；旧 `AGENTDOCK_ACP_ENV_FROM_ENV_JSON` 仅用于单 ACP 配置升级兼容。

## 配置 AgentDock

### macOS Desktop

优先使用 AgentDock.app 的高级设置：

1. 打开 **Coding Agent（ACP）**；
2. 勾选“启用 Coding Agent”；
3. 新增或选择一个 Profile；内置 Codex、Claude、Grok Build 各只能存在一个，Custom 可以创建多个；
4. Custom Profile 使用独立 ID，例如 `zcode`、`agy`，并配置对应 Adapter；
5. 选择一个已启用 Profile 作为默认 Profile，确认界面显示 Adapter 可用后保存设置。

macOS Desktop 会根据预设自动解析实际 Adapter 路径和参数，原子更新 AgentDock 的运行环境并重启 Core。模型如果有真实桌面操作能力，应直接完成这些操作，而不是让用户代做。

### Windows Desktop

优先使用 AgentDock 控制面板管理 ACP Profiles。内置 Codex、Claude、Grok Build 使用固定 Profile ID 且各只能存在一个；Custom 可以创建多个独立 ID。Windows Desktop 会在 PATH、用户 npm 目录、WinGet 链接、Grok 安装目录等位置解析 Adapter；Codex / Claude 也会识别对应 npm package 的 Node.js 入口。

如果必须使用 `agentdock config update`，先读取当前完整控制面板配置，再把端口、日志、浏览器、MCP Apps 等现有设置连同 ACP 设置一起提交；不要只传 ACP 参数导致其他桌面设置被默认值覆盖。

### Linux、Docker 和直接运行二进制

无桌面控制器时，ACP 属于 Core 启动环境。当前正式配置统一使用 Profiles，即使只启用一个 ACP 也使用同一模型：

```text
AGENTDOCK_ACP_ENABLED=true
AGENTDOCK_ACP_PROFILES_JSON=[{"id":"zcode","kind":"custom","command":"/absolute/path/to/node","args":["/absolute/path/to/zcode-acp-server.js"],"enabled":true}]
AGENTDOCK_ACP_DEFAULT_PROFILE=zcode
```

同时启用多个 ACP 时只需继续向 `AGENTDOCK_ACP_PROFILES_JSON` 增加 Profile。例如 Codex + ZCode：

```text
AGENTDOCK_ACP_PROFILES_JSON=[{"id":"codex","kind":"codex","command":"/absolute/path/to/codex-acp","enabled":true},{"id":"zcode","kind":"custom","command":"/absolute/path/to/node","args":["/absolute/path/to/zcode-acp-server.js"],"enabled":true}]
AGENTDOCK_ACP_DEFAULT_PROFILE=zcode
```

配置写入哪个文件、Compose environment 或进程管理器，继续按本 Skill 对应平台 reference 的“真实配置事实源”处理，不要发明统一 `agentdock.yaml`。

旧 `AGENTDOCK_ACP_AGENT/COMMAND/ARGS_JSON/ENV_FROM_ENV_JSON` 仅作为升级兼容入口：Core 读取后会立即转换成一个 Profile；新版 macOS/Windows Desktop 不再写这些字段。旧 `custom` 会迁移为 `id=custom`，因此原持久会话 identity 保持不变。

Profile 规则：

- `codex`、`claude`、`grok` 是内置类型的固定 ID，因此天然保持单实例；
- `custom` 可以配置多个，但每个 Profile ID 必须唯一；
- 每个启用 Profile 使用独立 ACP Manager 和持久会话 identity，session/run/interaction 不跨 Profile 混用；

保存后重启或重建真正承载 AgentDock Core 的运行单元，让新环境重新加载。

## ACP 工具模型

AgentDock 对外保留稳定的管理语义，不把 ACP 协议的每个底层方法直接暴露成一个 action：

| 工具 | 公开 action | 语义 |
| --- | --- | --- |
| `acp_session` | `info`、`new`、`list`、`inspect`、`open`、`update`、`close`、`delete` | 管理 AgentDock session 与 Adapter 原生 session |
| `acp_prompt` | `start`、`events`、`cancel` | 启动异步 Run、增量读取 Run 事件、请求取消 |
| `acp_interaction` | `list`、`respond` | 处理需要用户参与的交互；当前稳定支持 permission |

关键规则：

- `acp_session list` 返回一个统一的 `sessions[]`。AgentDock 已管理的会话保留 `session_id=acps_*` 并标记 `managed=true`；只有 Adapter 原生存在、尚未纳管的会话以 `source=remote` 返回。原生发现来自标准 ACP `session/list`，相同 `remote_session_id` 只保留一行；Adapter 不支持时会明确返回 `remote_available=false`，不会伪造远端结果。
- `open` 可以接收 `session_id` 或 `remote_session_id`。打开原生 session 时只建立 `acps_* -> remote_session_id` 的轻量映射，不复制 transcript；后续自动优先使用 `session/resume`，必要时才使用 `session/load`。
- `new(from_session_id=...)` 表达 fork；只有 Adapter 广告 fork capability 时才执行。`update` 统一承载 session mode / config option 修改。
- `inspect(include_history=true)` 才显式读取历史。历史唯一事实源是 Adapter；AgentDock 通过标准 `session/load` 收集 Adapter replay 的公开 `session/update`，不把 Run 事件拼成第二份 transcript，也不暴露 `agent_thought_chunk`。
- `close` 释放 Adapter 侧资源但保持 AgentDock 映射可再次 `open`；`delete` 是真正删除，必须由 Adapter 广告 delete capability，不支持时不得静默退化成本地解绑。
- `acp_prompt start` 接收 ACP ContentBlock 数组。`text` 与 `resource_link` 是基线类型；`image`、`audio`、embedded `resource` 按 Adapter 的 `promptCapabilities` 校验。若当前 session 已有 active Run，AgentDock 只在 Adapter 支持 steering 时内部处理 steering，不再公开单独的 `steer` action。
- `cancel` 是取消请求而不是“立即把本地 Run 标成完成”。AgentDock 会先发送 ACP cancel，继续接收 Adapter 的尾部 `session/update`，等原 prompt 以 `stopReason=cancelled` 收敛后再把 Run 标记为 cancelled。
- Run 事件中的 ACP 原生更新标记 `source=acp`，AgentDock 自己生成的生命周期事件标记 `source=agentdock`，便于调用方区分协议事实和宿主编排事件。
- `acp_interaction respond` 的 permission 只能选择 Adapter 当前提供且本地策略允许的 option；取消使用 `response.action=cancel`。在完整的结构化 elicitation 生命周期实现之前，AgentDock 不向 Adapter 广告 elicitation capability。

如果 `info` 返回认证方法，可以直接调用 `info(auth_method_id=...)` 完成独立认证，也可以在其他 session action 上同时提供 `auth_method_id`，让 AgentDock 先认证再执行操作；认证仍由 Adapter / Provider 管理，AgentDock 不保存 Provider 密码。

## 验证 ACP 真的可用

至少完成下面几层验证：

1. **Adapter 层**：解析到的命令真实存在，并且由 AgentDock 的运行用户可执行；Codex / Claude 的 npm Adapter 还要确认 Node.js 入口真实存在。
2. **Core 层**：Core 重启后健康检查正常。
3. **上下文层**：重新读取 `agentdock_context`，确认出现 ACP 信息、`enabled=true`，并检查 `default_profile` 与 `profiles`。
4. **工具层**：当前 MCP 连接的 `tools/list` 应包含 `acp_session`、`acp_prompt` 和 `acp_interaction`。
5. **Adapter 启动层**：工具已可见时优先调用 `acp_session info`，确认 Adapter 能启动并返回实际能力 / 认证状态；需要登录时再按 Adapter 暴露的认证方式处理。

多 Profile 下，`acp_session`、`acp_prompt`、`acp_interaction` 都接受可选 `profile_id`。省略时走默认 Profile；成功响应会回显实际处理请求的 `profile_id`。对非默认 Profile 的后续 session/prompt/interaction 操作应持续传同一个 `profile_id`。

如果当前客户端是 ChatGPT，启用 ACP 会改变工具 Schema。Core 已正确启用后，还要到 GPT 的 AgentDock 插件页面点击**刷新**，再**新开会话**，否则旧会话可能继续使用不包含 ACP 工具的缓存 Schema。

不要把“配置文件已写入”“Core 已重启”当成 ACP 已完成。只有 Adapter 能启动，并且 MCP 客户端实际拿到 ACP 工具，才算配置闭环。

## 常见失败判断

- 找到 `codex`，但没有 `codex-acp`：安装 Codex ACP Adapter，而不是重复安装 Codex。
- 找到 `claude`，但没有 `claude-agent-acp`：安装 Claude ACP Adapter，而不是直接把 `claude` 当成 ACP 命令。
- 找到 `grok`：检查它是否支持并能运行 `agent stdio`，不额外安装 Codex / Claude 的 npm Adapter。
- Adapter 在用户 shell 可见、服务里不可见：检查 AgentDock 实际运行用户和 PATH，不要只在另一个登录 shell 中验证。
- `agentdock_context` 显示 ACP 已启用但 ChatGPT 没有 ACP 工具：先按 ChatGPT 工具 Schema 缓存流程刷新插件并新建会话。
- Adapter 启动后要求登录：使用 Provider 自己的登录 / ACP authentication 流程，不把凭据硬编码进 AgentDock 参数。

官方用户文档：<https://uvwt.github.io/agentdock-docs/zh-CN/docs/guides/coding-agents>
