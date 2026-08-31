<div align="center">

[English](./README.md) | 简体中文

<img src="./docs/assets/agentdock-logo.png" alt="AgentDock logo" width="128" />

# AgentDock MCP

**让 AI 的双手，真正触达你的每一台设备。**

打开网页版 ChatGPT，即可管理多台电脑与服务器：在真实设备上写代码、改配置、跑命令与部署，执行发生在你的机器上，不消耗Codex额度。


[在线文档](https://uvwt.github.io/agentdock-docs/zh-CN/) · [下载安装](https://github.com/uvwt/agentdock/releases) · [QQ群](https://qun.qq.com/universal-share/share?ac=1&authKey=Rp86bSzI7vqm87KoYlKawgsPZ440Ubhyezw6Qkgcn3JISwX3zXxsXkbS5598RrY5&busi_data=eyJncm91cENvZGUiOiIxMDgxMzM3MDE5IiwidG9rZW4iOiJ0Mlg1bUU1ZWtuZzF3SHJDT3pSaGsrOURIMlNYaXBlYllOUjNLZ1BUb1hzM2lJSTZjeVNldzU0ajl0SjRVZkx2IiwidWluIjoiMzIwMjA4ODAzMiJ9&data=W28mWvuqaLf_Fwnf0CgAJXuDs6l3A78V7AoWZnizPboCpKoQMzHzZ-UlluYo47U3tmIBHK2xIgWEVEJbTiGsPQ&svctype=4&tempid=h5_group_info)

[![CI](https://github.com/uvwt/agentdock/actions/workflows/ci.yml/badge.svg)](https://github.com/uvwt/agentdock/actions/workflows/ci.yml)
[![GitHub Release](https://img.shields.io/github/v/release/uvwt/agentdock?display_name=tag&logo=github)](https://github.com/uvwt/agentdock/releases)
[![Docker Hub](https://img.shields.io/docker/pulls/agentdockio/agentdock?logo=docker&label=Docker%20Hub)](https://hub.docker.com/r/agentdockio/agentdock)
[![GHCR](https://img.shields.io/badge/GHCR-ghcr.io%2Fuvwt%2Fagentdock-2496ED?logo=docker&logoColor=white)](https://github.com/uvwt/agentdock/pkgs/container/agentdock)
[![License](https://img.shields.io/github/license/uvwt/agentdock)](./LICENSE)

</div>

<p align="center">
  <img
    src="./docs/assets/agentdock-multi-device.png"
    alt="AgentDock：通过一个 AI 对话统一操控多台设备"
    width="100%"
  />
</p>

## AgentDock 是什么

AgentDock 是一个面向 AI Agent 的独立工具运行层。

它为本地电脑、远程服务器与容器环境提供统一、安全、可控的文件、命令、Git、Skill、MCP、浏览器自动化和任务执行能力。配置多台 AgentDock，还能跨设备协同，把原本要在多机之间来回切换的工作，收敛到一次对话里完成。

AgentDock 不提供聊天界面，也不负责模型推理。它专注于解决一件事：

> 让 AI Agent 在明确的权限边界内操作真实环境，并返回结构化、可追踪、可验证的执行结果。

```text
              ChatGPT / Claude / Codex
                        │
                        │ MCP（可接入多台）
          ┌─────────────┼─────────────┐
          ▼             ▼             ▼
   ┌───────────┐ ┌───────────┐ ┌───────────┐
   │ AgentDock │ │ AgentDock │ │ AgentDock │
   │  本机电脑  │ │  内网机器   │ │  云服务器  │
   └─────┬─────┘ └─────┬─────┘ └─────┬─────┘
         │             │             │
         ▼             ▼             ▼
   文件·命令·Git  穿透客户端等   转发·反代·部署
```

## 你可以用 AgentDock 做什么

- 用网页版 ChatGPT 直接管理多台电脑与服务器，无需分别 SSH 登录来回操作
- 在真实设备上写代码、改项目、跑测试并操作 Git，执行发生在本机或服务器，不依赖专用编程 Agent 额度
- 让 AI 管理 VPS、Docker 服务、反向代理和部署配置
- 让 AI 检查日志、进程、端口和真实运行状态
- 让 AI 操作登录后的网页与 macOS 桌面应用
- 配置多台 AgentDock，在一次对话中完成跨设备协同任务
- 通过 Skill 和动态 MCP 扩展外部能力
- 保存长时间任务的执行状态，并在中断后继续
- 用同一套工具模型连接 macOS、Linux、Windows 与容器环境
- 等等


## 快速开始

普通用户直接使用正式安装包即可，不需要下载源码、安装 Go 或自己构建 AgentDock。

完整步骤见 [安装 AgentDock](https://uvwt.github.io/agentdock-docs/zh-CN/docs/getting-started/install)。


| 平台 | 文档 |
| --- | --- |
| Docker | [Docker 快速部署](https://uvwt.github.io/agentdock-docs/zh-CN/docs/getting-started/docker) |
| Linux | [Linux 自动安装](https://uvwt.github.io/agentdock-docs/zh-CN/docs/getting-started/linux) |
| Linux / VPS | [systemd 部署](https://uvwt.github.io/agentdock-docs/zh-CN/docs/getting-started/vps) |
| macOS | [macOS 安装](https://uvwt.github.io/agentdock-docs/zh-CN/docs/getting-started/macos) |
| Windows | [Windows 图形安装程序](https://uvwt.github.io/agentdock-docs/zh-CN/docs/getting-started/windows) |


### 连接方式如何选择

- **仅本机**：客户端和 AgentDock 在同一台电脑上。
- **临时公网地址**：没有域名，但需要从 ChatGPT、手机或其他设备连接。地址可能在 Tunnel 重启后变化。
- **固定域名**：长期使用稳定公网地址，需要已接入 Cloudflare 的域名和 Tunnel Token。

安装完成后，从控制面板或终端取得 MCP 地址，以及 Bearer Token 或 OAuth 登录信息，再填入客户端的 MCP、Tools 或 Connectors 设置。公网访问必须保留认证，不要把凭据放进截图、Issue 或公开聊天。

## 接入 AI 客户端

AgentDock 通过 MCP Streamable HTTP 提供工具能力。下面是一个通用配置示例，具体字段格式取决于所使用的 AI 客户端：

```json
{
  "mcpServers": {
    "agentdock": {
      "url": "http://127.0.0.1:8765/mcp",
      "headers": {
        "Authorization": "Bearer <AGENTDOCK_AUTH_TOKEN>"
      }
    }
  }
}
```


## 核心能力

### 文件与命令

- UTF-8 文本读取、搜索、目录遍历和结构化修改
- 原子文件写入、路径边界和私密目录保护
- 有超时和输出边界的命令执行
- 标准输出、标准错误和退出码分离
- 长时间命令会话、PTY、会话观察、输入和停止
- 输出截断和敏感信息脱敏
- macOS、Linux、Windows 与 WSL 支持

#### 显式透传宿主环境变量

`exec_command` 默认只使用精简环境，不会完整继承 AgentDock 进程环境。宿主运行环境确实依赖额外变量时，可以配置“子进程变量名 -> AgentDock 宿主进程变量名”的显式映射：

```bash
export AGENTDOCK_COMMAND_ENV_FROM_ENV_JSON='{"NIX_LD":"NIX_LD","NIX_LD_LIBRARY_PATH":"NIX_LD_LIBRARY_PATH"}'
```

只有显式声明的变量会被复制；宿主变量不存在时会跳过。Skill 环境变量可以覆盖宿主映射值，单次 `exec_command.env` 又可以继续覆盖 Skill 环境。

使用 systemd 或 OpenRC 部署时，映射来源是 **AgentDock 服务进程自身的环境**，不会读取某个用户的登录 Shell。以启用 `nix-ld` 的 NixOS 为例，需要同时通过服务配置向 AgentDock 注入当前的 `NIX_LD`、`NIX_LD_LIBRARY_PATH`，再配置上述映射。NixOS 建议通过声明式服务配置提供这些值，不要把某一代 `/nix/store` 的绝对路径长期快照到手写配置里。

### Git 与 GitHub

- 仓库状态、差异和提交记录读取
- 分支、提交、拉取和推送
- GitHub 仓库访问检查
- 修改前状态检查和修改后差异验证

### Skill 与动态 MCP

官方与社区 Skill 源码统一维护在 [uvwt/agentdock-skills](https://github.com/uvwt/agentdock-skills)。本仓库只保留必须随 AgentDock 运行时发布的三个自举核心 Skill。

- Skill 包校验、安装、激活和回滚
- 稳定版、开发版、Canary 和固定版本通道
- Skill 独立环境变量与运行环境
- 动态 MCP Server 注册、启停、刷新和移除
- Streamable HTTP 与 stdio 传输
- 工具搜索、Schema 检查和受控调用
- MCP Server 之间的配置隔离

### 原生 ACP 

AgentDock 可以选择作为 ACP Client 原生托管本地 Coding Agent adapter。

- 桌面控制面板提供 Codex、Claude 和 Grok 预设；是否启用 ACP 以及选择哪个 adapter 由宿主配置决定。
- 使用 `acp_session` 创建和管理会话，使用 `acp_prompt` 发起并观察 prompt，使用 `acp_interaction` 响应 Agent 的权限请求。
- 可选 ACP 操作只会在已连接 adapter 声明相应能力时开放。
- ACP 工作目录遵循宿主进程或容器的安全边界，而不是 AgentDock 文件系统 allowlist。

### 浏览器与桌面自动化

- 浏览器会话启动、关闭和清理
- 页面跳转、点击、输入、选择和等待
- 页面文本、可交互元素、错误和网络响应检查
- 登录状态、持久化浏览器 Profile 和截图
- macOS 系统 Chrome 与桌面自动化支持

### 可恢复任务

- 持久化任务状态
- 明确的目标、步骤和完成条件
- 分阶段检查点
- 阻塞原因记录和中断恢复
- 最终审查与完成验证
- 可复用工作流模板

### Recall 与 NexusDock 集成

AgentDock 可以选择与 NexusDock 配对，将它作为多设备汇总入口：

- 长期项目记忆
- 运行手册和经验记录
- 工作流模板
- 私密笔记
- 多设备状态协同

## 运行目录

| 路径 | 用途 |
| --- | --- |
| `~/AgentDock` | 相对文件操作的默认工作目录 |
| `~/.agentdock` | AgentDock 状态、配置、会话和扩展数据 |

## 端口说明

Docker、原生安装和本地开发的默认 MCP 地址：

`http://127.0.0.1:8765/mcp`

端口可以通过配置调整，客户端应以实际部署配置为准。

公网部署必须启用 Bearer Token 或 OAuth 认证并使用 HTTPS，不要将未认证的 MCP 服务暴露到公网。

## 开发与贡献

提交代码前运行完整检查：

```bash
make check
```

项目使用 GitHub Actions 持续执行测试、静态检查、构建和发布验证。

用户文档独立维护在 [`uvwt/agentdock-docs`](https://github.com/uvwt/agentdock-docs)。修改用户可见行为、配置参数、安装方式或工具 Schema 时，应同步更新对应文档。

提交问题或功能建议请使用 [GitHub Issues](https://github.com/uvwt/agentdock/issues)。

## ♥️ 支持项目

<p>如果 <b>AgentDock</b> 对您有帮助，请考虑为它点个 <b>Star</b> ⭐，感谢您的支持！</p>
<table>
<thead>
<tr>
<th align="center">微信(WeChat)</th>
<th align="center">支付宝(Alipay)</th>
</tr>
</thead>
<tbody><tr>
<td align="center"><img src="./docs/assets/donation/wechat-cropped.JPG" alt="微信赞助二维码" height="200"></td>
<td align="center"><img src="./docs/assets/donation/alipay.JPG" alt="支付宝赞助二维码" height="200"></td>
</tr>
</tbody>
</table>

## 相关链接

- [在线文档](https://uvwt.github.io/agentdock-docs/zh-CN/)
- [文档源码](https://github.com/uvwt/agentdock-docs)
- [GitHub Releases](https://github.com/uvwt/agentdock/releases)
- [GitHub Container Registry](https://github.com/uvwt/agentdock/pkgs/container/agentdock)
- [Docker Hub](https://hub.docker.com/r/agentdockio/agentdock)
- [Linux Do](https://linux.do/)

## License

Apache License 2.0. See [LICENSE](./LICENSE).

## 交流反馈

[加入 QQ 群（1081337019）](https://qun.qq.com/universal-share/share?ac=1&authKey=Rp86bSzI7vqm87KoYlKawgsPZ440Ubhyezw6Qkgcn3JISwX3zXxsXkbS5598RrY5&busi_data=eyJncm91cENvZGUiOiIxMDgxMzM3MDE5IiwidG9rZW4iOiJ0Mlg1bUU1ZWtuZzF3SHJDT3pSaGsrOURIMlNYaXBlYllOUjNLZ1BUb1hzM2lJSTZjeVNldzU0ajl0SjRVZkx2IiwidWluIjoiMzIwMjA4ODAzMiJ9&data=W28mWvuqaLf_Fwnf0CgAJXuDs6l3A78V7AoWZnizPboCpKoQMzHzZ-UlluYo47U3tmIBHK2xIgWEVEJbTiGsPQ&svctype=4&tempid=h5_group_info)
