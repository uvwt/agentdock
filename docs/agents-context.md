# 全局与工作区 AGENTS.md 自动上下文

AgentDock Core 原生发现并读取规则文件，不依赖 ACP、Codex、NexusDock 或 Recall。规则文本不会作为命令执行，也不改变文件访问权限。

## 加载入口

MCP 服务创建时，把全局规则和默认工作区规则加入初始化 instructions，并标明来源、适用目录和“启动快照”。`agentdock_context` 每次调用重新读取文件，返回最新正文和状态，不依赖文件修改时间缓存。

```json
{}
```

空参数使用当前运行时默认工作目录。操作另一个项目或进入有独立规则的子目录前，传入目标目录：

```json
{"workdir":"C:\\projects\\example"}
```

`workdir` 接受既有 Host 目录、相对路径和 `~/` 路径。选择仅对本次上下文请求有效，不会修改命令工具的默认工作目录、持久化配置或其他客户端的工作区。后续 `exec_command` 等操作仍需传入对应的 `workdir` 或绝对文件路径。

规则文件创建、修改或删除后，再调用 `agentdock_context` 即可刷新，无需重启 Core。文件改变不会主动推送或追溯修改客户端已经收到的启动快照，也不会凭空获知用户在自然语言里切换了哪个项目。客户端应在开始项目操作、切换项目或已知规则变化时获取上下文，只在正文尚未提供或需要编辑规则时另行读取文件。

## 来源顺序与适用范围

1. 全局：显式配置的 `AGENTDOCK_INSTRUCTIONS_FILE`，否则 `${AGENTDOCK_HOME}/AGENTS.md`。默认 `AGENTDOCK_HOME` 为用户目录下的 `.agentdock`。
2. 工作区根目录的 `AGENTS.md`。
3. 从该根目录到所选目录之间各级子目录的 `AGENTS.md`，由外向内排列。

显式 instructions 文件替代自动全局来源，不与同一份自动全局正文重复合并。全局规则先应用，子目录规则只细化适用目录的项目行为，不得削弱全局安全约束或客户端的更高优先级指令。

工作区边界取最近的 `.git` 标记目录，兼容 Git worktree 的 `.git` 文件。没有遇到仓库边界时，若所选目录位于配置的默认目录内，则以默认目录为边界；否则只读取所选目录的规则。发现仓库时只检查祖先的 `.git` 元数据，不读取边界外的祖先 `AGENTS.md`。不递归扫描无关子目录、兄弟项目或全部磁盘。

自动发现拒绝规则文件本身的符号链接和其他非普通文件。显式 `AGENTDOCK_INSTRUCTIONS_FILE` 保留原有符号链接解析语义。根目录内的读取使用 `os.Root` 约束路径解析，并校验打开前后的文件身份。同一实际文件通过相同路径或硬链接出现多次时只提供一次正文；不同文件即使文本相同，也保留各自的作用域。

## 返回结构与错误处理

原有上下文字段不变，新增可选 `instruction_files`：

```json
{
  "instruction_files": {
    "auto_load": true,
    "workdir": "/projects/example/src",
    "workspace_root": "/projects/example",
    "files": [
      {
        "scope": "global",
        "path": "/home/example/.agentdock/AGENTS.md",
        "status": "loaded",
        "content": "全局规则正文",
        "sha256": "00ee6e16073bc20a849a2b38b9120a3cec0fce3aa573294222e4b78d675c0143",
        "size_bytes": 18
      }
    ]
  }
}
```

路径仅为示例。摘要与字节数对应未带换行的示例正文，真实响应按原始文件字节计算。

`status` 包括 `loaded`、`not_found`、`empty`、`duplicate`、`skipped` 和 `error`。只有 `loaded` 含可应用的正文。重复条目提供 `duplicate_of`，拒绝或读取失败提供 `reason`。缺失的默认文件不阻止工具工作。显式配置的 instructions 文件仍保留启动配置阶段的严格校验，不能用自动加载掩盖配置错误。

单文件最多 64 KiB，单次正文总预算 256 KiB，目录层级最多 64。只接受 UTF-8 文本，支持 UTF-8 BOM 和 CRLF，拒绝 NUL、损坏编码及非普通文件。超限文件整份跳过，不把截断内容当作完整规则。Unix 打开文件时使用非阻塞及禁止叶子符号链接标志，避免检查后被替换成 FIFO 时阻塞。

## 配置与兼容性

默认启用自动发现。设置 `AGENTDOCK_AGENTS_AUTOLOAD=false` 可禁用自动全局和工作区发现，但不会禁用显式 `AGENTDOCK_INSTRUCTIONS_FILE`。开关与全局路径属于启动配置，修改它们仍需按部署方式重启 Core；仅规则正文改变不需要重启。

旧的空参数 `agentdock_context` 调用继续有效。新增 `workdir` 和 `instruction_files` 仅扩展本地工具契约，既有字段及必需字段保持不变。Nexus 私有 `context.local` 不增加字段，使用原有 `rules` 数组携带带来源和作用域的规则正文；不改变共享 protocol 依赖。Nexus 统一入口对任意工作区选择的支持仍由其自身契约决定，不能假定旧版 Nexus 接受本地新增参数。

升级 Core 后，缓存工具定义的客户端需要刷新工具定义并重新连接或新建会话。单纯修改源码不会使已运行的旧版本获得此功能。

## 开发验证

```text
go test ./internal/agentinstructions ./internal/config ./internal/app ./internal/mcp
go vet ./...
go build -o ./bin/agentdock-context.exe ./cmd/agentdock
```

内存受限环境为命令进程设置 `GOMAXPROCS=2` 并给 Go 命令添加 `-p 1`，不要为运行测试关闭用户应用或更改系统配置。全局和工作区测试均使用临时目录，不读取测试机真实全局规则。
