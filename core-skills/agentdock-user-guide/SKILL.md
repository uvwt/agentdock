---
name: agentdock-user-guide
description: 当用户询问 AgentDock 是什么、如何使用、配置在哪里、不同平台或安装方式怎样修改配置并生效、如何重启或验证配置、如何发现并配置 Codex/Claude/Grok 等 Coding Agent 的 ACP，以及常见运行问题时使用；覆盖 macOS Desktop、Windows Desktop、Linux 服务、Docker 和直接运行二进制，不用于源码开发与贡献流程。
version: 1.1.0
---

# AgentDock User Guide

这是随 AgentDock 发布的官方用户指南 Skill。目标是让模型基于用户当前真实安装方式，正确解释和操作 AgentDock，而不是依赖固定路径、旧版本经验或猜测。

回答使用用户当前语言；命令、路径和配置键保持原样。

## AgentDock 是什么

AgentDock 是面向 AI Agent 的独立工具运行层。它把文件、命令、Git、Skill、动态 MCP、浏览器、任务等能力通过 MCP 提供给 ChatGPT、Claude、Codex 等客户端；它本身不是聊天界面，也不负责模型推理。

一个 AgentDock 实例对应一个实际运行环境。客户端可以连接本机 AgentDock，也可以连接远程服务器、容器或其他设备上的 AgentDock。多设备场景下，先确认当前工具实际连到哪一个实例，再解释或修改该实例的配置。

通常通过 AgentDock 暴露的 MCP 地址连接，默认 HTTP 端口是 8765，MCP 路径是 `/mcp`；实际地址、认证方式和端口始终以当前安装/部署配置为准。公网访问必须保留认证并使用 HTTPS，不要把 Token、OAuth 密码或其他凭据发到公开聊天、截图或 Issue。

如果当前 AgentDock 暴露 `agentdock_context`，把它作为识别版本、平台、运行目录、可用 Skill 与能力的首选入口；具体文件、进程、服务和容器状态仍应通过真实工具继续验证。

## AgentDock 生态项目

### NexusDock

[NexusDock](https://github.com/uvwt/nexusdock) 是 AgentDock 的自托管中心服务。它把多台 AgentDock、长期记忆和 Workflow 集中到一个入口；单台 AgentDock 仍可独立使用，NexusDock 不是运行 AgentDock 的必需组件。

以下场景尤其适合使用 NexusDock：

- **ChatGPT 只接入一个 MCP**：把多台 AgentDock 与 NexusDock 配对后，ChatGPT 只需要配置 NexusDock 的统一 `/mcp` 地址。NexusDock 会向客户端提供统一工具入口；需要执行某台设备上的文件、命令、浏览器、Skill、动态 MCP 等能力时，先通过 `agentdock_context` 确认节点，再用对应 `node_id` 把工具调用路由到那台 AgentDock。这样不需要在 ChatGPT 中分别维护多台设备的 MCP 连接。
- **需要长期记忆能力**：NexusDock 提供集中式 Recall 工作区和 `recall_*` 工具，用于搜索、读取、维护长期项目记忆、运行手册和经验记录。多台 AgentDock 可以使用同一份中心记忆，而不是各自在本机保存彼此不可见的长期上下文。
- **需要 Workflow 能力**：NexusDock 集中保存和匹配可复用 Workflow 模板，并通过 `workflow_template_manage` 提供给模型；多步骤任务仍可结合 `task_manage` 持久化执行进度。基础 Workflow 不依赖 Embedding，配置向量服务后还可以使用语义匹配。
- **需要多设备统一管理**：Web 控制台可以查看设备在线状态、版本和运行时能力，并集中查看 Recall、Workflow、任务、Skill 和动态 MCP 状态。

NexusDock 不替代设备端 AgentDock：实际文件、命令、浏览器、Skill、动态 MCP 等设备能力仍由目标 AgentDock 节点执行；NexusDock 负责中心管理、共享 Recall/Workflow、统一 MCP 接入和跨设备路由。只有一台 AgentDock、也不需要中心记忆或 Workflow 时，可以直接连接 AgentDock，不必额外部署 NexusDock。

统一 MCP 地址形式为：`https://<你的 NexusDock 域名>/mcp`。支持 OAuth 的客户端可以直接授权；其他客户端也可以使用 NexusDock 的 MCP Access Token。具体认证方式以 NexusDock 当前部署为准。

官方仓库：<https://github.com/uvwt/nexusdock>

### AgentDock Documentation

AgentDock 的用户文档独立维护在 [uvwt/agentdock-docs](https://github.com/uvwt/agentdock-docs)。安装、平台配置、部署、升级、工具和用户可见行为等说明应优先参考这里，而不是只依赖主仓库 README。

在线文档：<https://uvwt.github.io/agentdock-docs/zh-CN/>

文档源码仓库：<https://github.com/uvwt/agentdock-docs>

### AgentDock Skills

[AgentDock Skills](https://github.com/uvwt/agentdock-skills) 是 AgentDock 官方与社区 Skill 的源码、测试和发布仓库。普通业务集成、个人效率工具和社区 Skill 在这里独立维护和版本化，避免与 AgentDock Core 版本强耦合。

AgentDock 主仓库的 `core-skills/` 只保留必须随 AgentDock 运行时一起安装和升级的内置核心 Skill；需要查找、阅读、贡献或发布其他 Skill 时，应优先查看 AgentDock Skills 仓库。安装第三方或社区 Skill 前仍应进行来源和安全审查。

官方仓库：<https://github.com/uvwt/agentdock-skills>

### ChatGPT 的工具 Schema 缓存

使用 ChatGPT 平台连接 AgentDock 时，工具定义还存在一层平台侧缓存。AgentDock 已经完成工具变更，不代表当前 ChatGPT 会话会立即拿到新的 Schema。

以下变化都应按“工具 Schema 已变更”处理：

- 新增或删除工具；
- 修改工具名称、描述、输入参数或输出 Schema；
- AgentDock 升级后导致当前暴露的 MCP 工具集合发生变化。

让变更在 ChatGPT 中生效时：

1. 先确认 AgentDock Core 已经升级/重启并暴露新的工具定义；
2. 进入 GPT 的 AgentDock 插件页面，点击**刷新**按钮，让 ChatGPT 重新获取 AgentDock 的工具 Schema；
3. **新开一个会话**再进行验证。旧会话可能继续使用创建会话时缓存的旧 Schema。

因此，如果出现“AgentDock 代码和 Core 都已经更新，但 ChatGPT 仍看不到新工具/新参数”的情况，不要继续反复重启 Core；先检查 ChatGPT 侧是否已经刷新插件并新建会话。这个缓存属于 ChatGPT 平台侧，不是 AgentDock 本地配置未生效。

## 适用场景

使用本 Skill 处理：

- AgentDock 的定位、能力和基本使用方式；
- “配置文件在哪里”“这个配置怎么改”“改完为什么没生效”；
- macOS、Windows、Linux、Docker、直接运行二进制之间的配置差异；
- Core 的启动、停止、重启、健康检查和配置生效验证；
- 浏览器、MCP Apps UI、端口、日志、OAuth 等运行配置的入口；
- 发现本机已有 Codex、Claude、Grok 等 Coding Agent，补齐缺失 ACP Adapter，并把 ACP 正确接入 AgentDock；
- 多台 AgentDock 设备中，确认应该修改哪一台设备的运行配置。

不要用本 Skill 代替：

- AgentDock 源码开发、重构、测试和 PR 流程；
- 第三方 Skill 的创作或安全审查；
- NexusDock、ChatDock 等其他项目自己的配置说明。

## 最重要的配置模型

AgentDock Core 在启动时从**进程环境**读取运行配置。不同发行方式负责把自己的持久化配置转换成 Core 的启动环境：

- macOS Desktop：AgentDock.app 管理自己的运行环境文件；
- Windows Desktop：控制面板设置、运行清单和受保护凭据共同生成 Core 环境；
- Linux 服务：systemd/OpenRC 从安装时选择的环境文件加载配置；
- Docker：容器的 environment / env_file 等启动配置决定 Core 环境；
- 直接运行二进制：当前 shell 或进程管理器提供环境。

因此：

1. **不存在一个适用于所有平台的 `agentdock.yaml`。不要创建或建议创建它。**
2. `~/.agentdock` 主要是 AgentDock 状态、Skill、会话等数据目录，不等于所有平台的 Core 运行配置文件。
3. 改文件不等于已生效。大多数运行配置只有在对应 Core 进程重新启动后才会重新读取。
4. 多设备之间的运行配置默认彼此独立。修改前先确认目标设备，不要假定一个设备的配置会同步到其他设备。

常用运行配置和边界见 `references/configuration.md`。

## 标准处理流程

### 1. 先确认真实运行环境

优先确认以下事实：

- 当前操作的是哪台 AgentDock 设备；
- Core 实际运行在 macOS、Windows、Linux 还是容器里；
- 安装方式是 Desktop、Linux 服务、Docker，还是直接启动二进制；
- 当前 AgentDock 版本；
- Core 当前是否运行、健康检查是否通过。

如果宿主提供 `agentdock_context`，优先用它读取当前设备的 `os`、`version`、AgentDock 状态目录和默认工作目录。它只能帮助确认当前 AgentDock 实例，不能替代对 Desktop runtime、service unit、Compose 配置等真实启动来源的检查。

不要因为用户在 macOS 或 Windows 上聊天，就推断 AgentDock Core 一定直接运行在该系统：它也可能实际运行在 Docker、WSL、远程 Linux 或另一台设备。

### 2. 按安装方式读取对应说明

- macOS Desktop：`references/macos.md`
- Windows Desktop：`references/windows.md`
- Linux systemd/OpenRC/手工服务：`references/linux.md`
- Docker / Docker Compose：`references/docker.md`
- Coding Agent / ACP：`references/acp.md`
- 直接运行 `agentdock`：继续使用本文件的“直接运行二进制”说明

只读取与当前环境相关的 reference；处理 ACP 时除平台 reference 外，再读取 `references/acp.md`。不要一次把所有平台的命令都丢给用户选择。

### 3. 修改前先查看现状

涉及配置变更时，先读取当前真实配置来源，再决定修改方式：

- Desktop 优先使用官方设置界面；
- Linux 先确认 service unit 实际引用的 EnvironmentFile；
- Docker 先读取当前 Compose/container 配置；
- 直接运行二进制先确认启动命令和进程环境来源。

不要只根据默认路径直接覆盖文件。安装器通常允许用户改变安装目录、服务名、环境文件路径或容器配置。

涉及认证 Token、OAuth 密码、签名密钥、Tunnel Token 等秘密时，只确认“是否已配置”或使用安全的专用配置入口，不在回复、日志或截图中回显真实值。

### 4. 让配置真正生效

配置修改后，重启**实际承载 Core 的运行单元**：

- Desktop：由桌面控制器保存并重启，或按平台 reference 使用对应 Core 重启方式；
- systemd/OpenRC：重启实际 AgentDock service；
- Docker：环境或 Compose 配置变化后重建/重建容器，不能只依赖 `docker compose restart`；
- 直接运行二进制：结束旧进程，并从新环境重新启动。

如果只修改 Skill 环境、动态 MCP 注册等由 AgentDock 自己管理的独立状态，应使用对应 Tool 的管理能力，不要误重写 Core 运行配置。

### 5. 修改后必须验证

至少验证两层：

1. **进程层**：Core 已运行，`/healthz` 正常，或平台 service status 显示 healthy；
2. **行为层**：本次修改对应能力真的变化，例如端口、MCP Apps UI、browser、ACP 或子进程环境行为符合预期。

如果当前对话连接的就是被重启的 AgentDock，连接可能短暂中断；恢复后重新读取 `agentdock_context` 或实际状态，不要把“命令执行成功”当成“新配置已经生效”。

配置失败时保留原配置和错误证据，优先恢复到已知可工作的配置，再报告具体失败点。

## 直接运行二进制

直接从终端、脚本或其他进程管理器启动 `agentdock` 时，没有额外的通用持久化配置层：Core 使用启动进程收到的环境变量。

处理方式：

1. 找出真正负责启动 `agentdock` 的 shell、脚本或进程管理器；
2. 在那个启动来源里修改环境变量，而不是只在另一个终端临时 `export`；
3. 停止旧 Core；
4. 从更新后的环境重新启动；
5. 检查 `/healthz` 和目标功能。

如果用户实际使用 systemd、OpenRC、Docker 或 Desktop，不要按“直接运行二进制”处理。

## 回答要求

给用户的最终操作说明应尽量包含：

- 当前识别出的平台和安装方式；
- 实际配置事实源；
- 要修改的具体设置；
- 正确的生效动作；
- 一条明确的验证方法；
- 如果发现当前环境与预期不一致，说明证据，而不是继续猜测。

涉及修改时优先直接使用当前可用 AgentDock 工具检查和验证真实环境；只有缺少操作能力时，才让用户手工执行必要步骤。
