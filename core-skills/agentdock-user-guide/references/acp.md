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

不要在聊天、日志或配置文件中回显 Provider Token。Codex、Claude、Grok 的账号登录由对应 Provider 自己管理；AgentDock 只负责启动 Adapter。只有确实需要把宿主环境变量映射给自定义 Adapter 时才使用 `AGENTDOCK_ACP_ENV_FROM_ENV_JSON`，并且只保存变量名映射，不写入 secret 值。

## 配置 AgentDock

### macOS Desktop

优先使用 AgentDock.app 的高级设置：

1. 打开 **Coding Agent（ACP）**；
2. 勾选“启用 Coding Agent”；
3. 选择 Codex、Claude 或 Grok Build；
4. 确认界面显示“已检测到”对应 Adapter；
5. 保存设置。

macOS Desktop 会根据预设自动解析实际 Adapter 路径和参数，原子更新 AgentDock 的运行环境并重启 Core。模型如果有真实桌面操作能力，应直接完成这些操作，而不是让用户代做。

### Windows Desktop

优先使用 AgentDock 控制面板选择 Codex、Claude 或 Grok Build。Windows Desktop 会在 PATH、用户 npm 目录、WinGet 链接、Grok 安装目录等位置解析 Adapter；Codex / Claude 也会识别对应 npm package 的 Node.js 入口。

如果必须使用 `agentdock config update`，先读取当前完整控制面板配置，再把端口、日志、浏览器、MCP Apps 等现有设置连同 ACP 设置一起提交；不要只传 ACP 参数导致其他桌面设置被默认值覆盖。

### Linux、Docker 和直接运行二进制

无桌面控制器时，ACP 属于 Core 启动环境。至少需要：

```text
AGENTDOCK_ACP_ENABLED=true
AGENTDOCK_ACP_AGENT=<codex|claude|grok|custom>
AGENTDOCK_ACP_COMMAND=<Adapter 的绝对可执行路径>
```

Codex / Claude 通常把 `AGENTDOCK_ACP_COMMAND` 指向真实可执行的 `codex-acp` / `claude-agent-acp`。Grok 还需要：

```text
AGENTDOCK_ACP_ARGS_JSON=["agent","stdio"]
```

自定义 Adapter 使用自己的绝对命令和参数。配置写入哪个文件、Compose 环境或进程管理器，继续按本 Skill 对应平台 reference 的“真实配置事实源”处理，不要发明统一 `agentdock.yaml`。

保存后重启或重建真正承载 AgentDock Core 的运行单元，让新环境重新加载。

## 验证 ACP 真的可用

至少完成下面几层验证：

1. **Adapter 层**：解析到的命令真实存在，并且由 AgentDock 的运行用户可执行；Codex / Claude 的 npm Adapter 还要确认 Node.js 入口真实存在。
2. **Core 层**：Core 重启后健康检查正常。
3. **上下文层**：重新读取 `agentdock_context`，确认出现 ACP 信息、`enabled=true`，且 `agent` 是预期值。
4. **工具层**：当前 MCP 连接的 `tools/list` 应包含 `acp_session`、`acp_prompt` 和 `acp_interaction`。
5. **Adapter 启动层**：工具已可见时优先调用 `acp_session info`，确认 Adapter 能启动并返回实际能力 / 认证状态；需要登录时再按 Adapter 暴露的认证方式处理。

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
