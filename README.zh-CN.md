<div align="center">

[English](./README.md) | 简体中文

<img src="./packaging/assets/agentdock.png" alt="AgentDock logo" width="128" />

# AgentDock MCP

**让 AI 的双手，真正触达你的每一台设备。**

打开网页版 ChatGPT，即可管理多台电脑与服务器：在真实设备上写代码、改配置、跑命令与部署，执行发生在你的机器上，不消耗Codex额度。

<a href="https://trendshift.io/repositories/136526?utm_source=trendshift-badge&amp;utm_medium=badge&amp;utm_campaign=badge-trendshift-136526" target="_blank" rel="noopener noreferrer"><img src="https://trendshift.io/api/badge/trendshift/repositories/136526/daily?language=Go" alt="uvwt%2Fagentdock | Trendshift" width="250" height="55"/></a>

[在线文档](https://docs.nexusdock.co/agentdock/) · [下载安装](https://www.nexusdock.co/download/) · [QQ群](https://qun.qq.com/universal-share/share?ac=1&authKey=Rp86bSzI7vqm87KoYlKawgsPZ440Ubhyezw6Qkgcn3JISwX3zXxsXkbS5598RrY5&busi_data=eyJncm91cENvZGUiOiIxMDgxMzM3MDE5IiwidG9rZW4iOiJ0Mlg1bUU1ZWtuZzF3SHJDT3pSaGsrOURIMlNYaXBlYllOUjNLZ1BUb1hzM2lJSTZjeVNldzU0ajl0SjRVZkx2IiwidWluIjoiMzIwMjA4ODAzMiJ9&data=W28mWvuqaLf_Fwnf0CgAJXuDs6l3A78V7AoWZnizPboCpKoQMzHzZ-UlluYo47U3tmIBHK2xIgWEVEJbTiGsPQ&svctype=4&tempid=h5_group_info)

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
    width="800"
  />
</p>

## AgentDock 是什么

AgentDock 是连接 AI 助手与真实设备的桥梁，让 AI 在你的电脑和服务器上执行任务。

它为本地电脑、远程服务器与容器环境提供统一、安全、可控的文件、命令、Git、Skill、MCP、浏览器自动化和任务执行能力。配置多台 AgentDock，还能跨设备协同，把原本要在多机之间来回切换的工作，收敛到一次对话里完成。

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
- 让 AI 操作登录后的网页与 macOS 桌面应用
- 通过 Skill 和动态 MCP 扩展外部能力
- 保存长时间任务的执行状态，并在中断后继续
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


### 连接设备

macOS 和 Windows 用户可在控制面板的**连接**页配对到官方远程服务：前往 [NexusDock 设备页](https://mcp.nexusdock.co/workspace/devices)获取配对码，输入后点击**连接**。也可以选择自托管服务。

需要本地 MCP 或公网直连时，进入**设置 → 高级连接**，也可从连接页的**高级连接设置**入口进入。在这里查看本地 MCP 地址和访问凭据，并配置 Cloudflare 临时公网地址或固定域名。Linux 和 Docker 用户按安装文档配置连接。

Windows 自定义数据目录安装会在该目录中完成配对并读取设备身份。macOS 页面后台刷新保留未保存的编辑；自定义编程助手名称支持中文，内部 Profile ID 自动生成。

## MCP 直连

选择直连时，将 MCP 地址及 Bearer Token 或 OAuth 登录信息填入客户端的 MCP、Tools 或 Connectors 设置。公网访问必须保留认证，不要把凭据放进截图、Issue 或公开聊天。

AgentDock 使用 MCP Streamable HTTP。下面是本地连接的通用配置示例，具体字段格式取决于所使用的 AI 客户端：

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

- **文件与命令**：在设备上读写文件、搜索代码、执行命令。
- **多设备管理**：连接本机、服务器和容器，跨设备完成任务；可搭配 NexusDock 统一接入并保存项目记忆。
- **浏览器与桌面**：操作网页、复用登录状态、截图，以及控制 macOS 桌面应用。
- **能力扩展**：通过 Skills 和 MCP 接入更多工具，官方与社区 Skills 见 [agentdock-skills](https://github.com/uvwt/agentdock-skills)。
- **编程助手**：连接 Codex、Claude 等受支持的编程助手，在设备上开发项目。
- **任务恢复**：保存进度和检查点，让长时间任务在中断后继续执行。

配置与使用详情见[在线文档](https://docs.nexusdock.co/agentdock/)。

## 连接恢复

核心服务运行状态与远程连接状态分别显示。NexusDock 连接在握手超时或对端失去响应后自动重连。Windows 本地控制连接卡住时，不阻塞其他控制请求或后台退出；状态命令等待两秒后回退到系统查询。

Windows 停止与重启会清理同一安装目录内的旧版本 Core 残留。核心不健康时，启动操作先等待旧进程退出，再拉起当前版本，避免旧进程继续占用端口。

Windows 固定域名 Cloudflare Tunnel 检测实际连接健康；VPN、DNS 或网络切换后持续断连时，会自动重建隧道进程，并在控制面板和托盘显示重连状态。恢复采用退避与限流，主动停止会取消后台恢复。

动态 MCP 连接失效后会清理会话，下一次独立调用可重新连接。ACP 管道写入和排队等待支持取消。MCP 凭据仅发送给配置的同源地址；清除 OAuth 授权后，正在进行的换码或刷新不能恢复旧授权。

超时规则、本地 API 访问限制与回归检查见[运行时访问与连接恢复](./docs/runtime-access-and-recovery.md)。

## 运行目录

| 路径 | 用途 |
| --- | --- |
| `~/AgentDock` | 相对文件操作的默认工作目录 |
| `~/.agentdock` | AgentDock 状态、配置、会话和扩展数据 |

## 感谢赞助商

| <div align="center"><strong>🏆&nbsp;赞助商&nbsp;🏆</strong></div> | <div align="center"><strong>介绍</strong></div> |
| :------------- | :------ |
| <a href="https://www.swiftproxy.net/?ref=agentdock"><img src="./docs/assets/sponsors/swiftproxy-logo.png" width="180" alt="Swiftproxy"></a> | <br>[**Swiftproxy**](https://www.swiftproxy.net/?ref=agentdock)<br>提供高质量动态和静态住宅代理，拥有 9000 万+住宅 IP，覆盖全球 220+ 个地区，支持 HTTP(S)、SOCKS5、IP 轮换与粘性会话，为 AI Agent、浏览器自动化和网页数据采集提供灵活的代理支持。动态住宅代理低至 $0.7/GB，使用优惠码 <code>PROXY90</code> 享受九折优惠。 |

想支持项目或展示品牌？[联系维护者](mailto:t80980@gmail.com)。

## 支持项目

<p>如果 <b>AgentDock</b> 对您有帮助，请考虑为它点个 <b>Star</b> ⭐，感谢您的支持</p>

| 微信赞赏 | 支付宝 | Buy Me a Coffee |
| :---: | :---: | :---: |
| <a href="./docs/assets/donation/wechat-qr.png"><img src="./docs/assets/donation/wechat-qr.png" alt="微信赞赏" width="140" height="140"></a> | <a href="./docs/assets/donation/alipay-qr.png"><img src="./docs/assets/donation/alipay-qr.png" alt="支付宝" width="140" height="140"></a> | <a href="https://buymeacoffee.com/uvwt"><img src="https://cdn.buymeacoffee.com/buttons/v2/default-yellow.png" alt="Buy me a coffee" width="180"></a> |

## Star 趋势

[![Star History Chart](https://api.star-history.com/svg?repos=uvwt/agentdock&type=Date)](https://star-history.com/#uvwt/agentdock&Date)

## 相关链接

- [Documentation](https://docs.nexusdock.co/agentdock/)
- [GitHub Releases](https://github.com/uvwt/agentdock/releases)
- [Linux Do](https://linux.do/)

## License

Apache License 2.0. See [LICENSE](./LICENSE).

## 交流反馈

[加入 QQ 群（1081337019）](https://qun.qq.com/universal-share/share?ac=1&authKey=Rp86bSzI7vqm87KoYlKawgsPZ440Ubhyezw6Qkgcn3JISwX3zXxsXkbS5598RrY5&busi_data=eyJncm91cENvZGUiOiIxMDgxMzM3MDE5IiwidG9rZW4iOiJ0Mlg1bUU1ZWtuZzF3SHJDT3pSaGsrOURIMlNYaXBlYllOUjNLZ1BUb1hzM2lJSTZjeVNldzU0ajl0SjRVZkx2IiwidWluIjoiMzIwMjA4ODAzMiJ9&data=W28mWvuqaLf_Fwnf0CgAJXuDs6l3A78V7AoWZnizPboCpKoQMzHzZ-UlluYo47U3tmIBHK2xIgWEVEJbTiGsPQ&svctype=4&tempid=h5_group_info)
