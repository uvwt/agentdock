# Dynamic MCP

AgentDock 将动态 MCP 与内置工具分开：内置工具直接调用，远端 MCP 工具不会进入 AgentDock 对外的动态 `tools/list`，而是通过四个固定入口按需发现和调用。这样新增、删除或更新 MCP Server 时，不需要依赖上层客户端刷新缓存的工具定义。

## 模型调用链

```text
agentdock_context
→ 读取已启用 MCP 的 name / description
→ mcp_tool_search
→ mcp_tool_inspect
→ mcp_tool_call
```

固定入口：

- `mcp_manage`：注册、查看、启用、禁用、刷新和删除 MCP Server。
- `mcp_tool_search`：按 Server 和关键词搜索轻量工具摘要。
- `mcp_tool_inspect`：读取 `<server>:<tool>` 的完整输入 Schema。
- `mcp_tool_call`：校验参数后代理上游 `tools/call`。

`agentdock_context` 只展示已启用 MCP 的名称和描述，不展示 URL、传输方式、工具清单、Schema 或鉴权配置。

## 注册 Streamable HTTP MCP

```json
{
  "action": "add",
  "name": "figma",
  "description": "访问 Figma 设计文件、节点、截图和设计上下文。",
  "transport": "streamable_http",
  "url": "https://mcp.example.com/mcp",
  "header_env": {
    "Authorization": "FIGMA_MCP_AUTHORIZATION"
  },
  "enabled": true,
  "timeout_ms": 30000
}
```

`header_env` 的键是 HTTP Header，值是 AgentDock 进程中的环境变量名。注册表只保存环境变量名，不保存 Token 或 Header 值。环境变量应包含完整 Header 值，例如 `Bearer ...`。

AgentDock 支持 JSON 和 `text/event-stream` 响应、MCP Session ID、分页 `tools/list` 以及初始化协商后的协议版本。

## 注册 stdio MCP

```json
{
  "action": "add",
  "name": "local-files",
  "description": "提供本地文件分析 MCP 工具。",
  "transport": "stdio",
  "command": "/absolute/path/to/mcp-server",
  "args": ["--stdio"],
  "cwd": "/absolute/working/directory",
  "env_from_env": {
    "SERVICE_TOKEN": "LOCAL_FILES_MCP_TOKEN"
  },
  "enabled": true
}
```

`env_from_env` 的键是子进程环境变量名，值是 AgentDock 进程中的宿主环境变量名。stdio Server 以持久子进程运行，禁用、删除、刷新或 AgentDock 退出时会回收整个进程树。

## 发现和调用

搜索：

```json
{
  "server": "figma",
  "query": "screenshot",
  "limit": 10
}
```

已明确 Server、但不知道工具名时，可以使用 `"query": "*"` 列出该 Server 的轻量工具摘要。

检查：

```json
{
  "name": "figma:get_screenshot"
}
```

调用：

```json
{
  "name": "figma:get_screenshot",
  "arguments": {
    "file_key": "...",
    "node_id": "1:2"
  }
}
```

AgentDock 在转发前使用发现到的输入 Schema 校验常见结构关键词，包括类型、必填字段、对象属性、附加属性、数组元素、枚举、长度、数量边界和 `allOf` / `anyOf` / `oneOf`。上游 MCP Server 仍是最终业务参数校验方。

远端返回的 MCP `content` 会原样透传，包括文本、图片和资源内容块；完整结果同时保留在 `structuredContent` 中。

## 管理动作

```text
mcp_manage list
mcp_manage inspect
mcp_manage add
mcp_manage enable
mcp_manage disable
mcp_manage refresh
mcp_manage remove
```

`refresh` 会断开当前客户端、重新初始化 Server、重新执行 `tools/list` 并替换内存中的 Tool Schema 缓存。普通搜索、检查或调用在尚未建立连接时会按需初始化。

注册表位于：

```text
~/.agentdock/mcp/servers.json
```

文件使用原子写入并设置为仅当前用户可读写。注册表中不得出现明文 Token、Cookie、OAuth Code 或密码。

## 当前边界

- Dynamic MCP 是独立底层能力，不包含 Plugin、Connector、Marketplace 或 Hook 概念。
- HTTP 鉴权当前通过 Header 对宿主环境变量的映射完成；OAuth 授权流程不在本模块中实现。
- AgentDock 不把远端 Tool Definition 动态加入对外 `tools/list`，避免上层客户端工具缓存失效。
- Skill 仍然只是说明文档：模型读取 `SKILL.md` 后，可以直接使用内置工具，也可以按上述流程使用动态 MCP。
