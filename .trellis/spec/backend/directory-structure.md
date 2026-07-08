# 目录结构

> AgentDock 后端代码组织方式。

## 概览

AgentDock 是单一 Go module。公开可执行入口位于 `cmd/agentdock`；可复用实现细节位于 `internal/`。包边界应保持窄而清晰，包名按职责命名，避免泛化成 `utils` 或 `helpers` 这类层级。

## 目录布局

```text
cmd/agentdock/                 CLI 与进程启动入口
internal/auth/                 Bearer 与 OAuth helper
internal/commandqueue/         Nexus 命令队列状态和执行
internal/compatenv/            兼容环境变量定义
internal/config/               运行时配置、环境默认值、路径策略
internal/envregistry/          本地 Skill 环境变量注册表
internal/httpx/                HTTP 服务、OAuth 端点、artifact 文件服务
internal/jsonrpc/              JSON-RPC 请求/响应 helper
internal/logx/                 基于 slog 的项目日志封装
internal/mcp/                  MCP 服务、工具描述、schema
internal/nexusagent/           本地 Nexus agent 循环和 adapter
internal/nexusclient/          Nexus client、heartbeat、本地状态
internal/sandbox/              Landlock 和平台 sandbox 集成
internal/session/              长时间运行的命令会话
internal/skillruntime/         原生 Skill 包解析、安装、执行
internal/skillstate/           active Skill 版本状态
internal/textutil/             小型文本 helper
internal/tools/                MCP 工具运行时和工具实现
internal/workspace/            workspace 与 host 路径解析
docs/                          产品和运维文档
scripts/                       本地安装、重启和 smoke 脚本
```

## 模块组织

- `cmd/agentdock` 负责串联配置、日志、runtime 构造、可选 Nexus agent 启动和 server 启动。业务逻辑不要放进 `main.go`。
- `internal/config` 负责环境变量、命令行默认值、归一化、runtime mode、sandbox mode 和 path policy。
- `internal/mcp` 负责 MCP 协议行为、工具描述和 JSON schema 对外表面。工具变化时，registry、input schema、output schema 和测试要一起更新。
- `internal/tools` 负责工具 dispatch 和工具实现。工具函数应校验输入，通过 `internal/workspace` 解析路径，返回结构化结果，并用 `ToolError` 表达客户端可见失败。
- `internal/skillruntime` 是原生 Skill Runtime。新增具体应用自动化能力应作为包/runtime 支持或外部 Skill 包接入，不走旧动态 plugin 路径。
- `internal/httpx` 负责 HTTP 端点，不得把 auth header、OAuth code 或 request body 泄露到日志。
- `internal/nexus*` 包负责设备控制面集成，contract DTO 的使用应尽量集中。

## 命名约定

- 包名使用简短小写，不使用下划线。
- 文件名优先描述功能或边界，例如 `skill_manage.go`、`input_schema.go`、`server_test.go`。
- 测试文件放在被测包旁边，使用 `_test.go`。
- 除非包本身已经足够窄，否则避免 `utils.go` 这类兜底文件。
- profile 名称、runtime mode、sandbox mode 和协议级名称使用常量。

## 示例

- `internal/tools/workspace_edit.go` 把工作区编辑的校验、精确匹配语义和诊断细节放在统一工具实现附近。
- `internal/skillruntime/manifest.go` 集中处理 manifest 解析和校验，避免在 install 与 execution 路径中分散校验。
- `internal/mcp/registry_test.go` 校验对外工具的 profile 和 descriptor 不变量。
