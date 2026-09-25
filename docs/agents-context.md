# Workspace 上下文

AgentDock 把设备级能力总览与项目级规则分开处理：`agentdock_context` 只返回 Runtime、Skill、动态 MCP、Workflow、Recall、ACP 和稳定操作规则；项目规则与工作区 Skill 由独立的 `workspace_context` 按请求读取。

## 规则文件

AgentDock 只识别两类 `AGENTS.md`：

1. 全局规则固定为 `~/.agentdock/AGENTS.md`；不提供自定义全局规则路径。
2. 工作区规则从 workspace root 的 `AGENTS.md` 开始，到本次 `workdir` 之间逐级继承子目录 `AGENTS.md`。

workspace root 优先使用离 `workdir` 最近的 Git / worktree 边界；在 AgentDock 默认工作目录内部且没有更近 Git 边界时，以默认工作目录为边界；两者都不存在时只检查当前 `workdir`。

`workspace_context` 每次调用都重新读取文件，不使用 mtime 缓存，也不会修改进程 cwd、AgentDock 默认工作目录或后续命令的默认目录。开始操作具体项目、切换工作区或规则可能变化时应重新调用。

## workspace_context

直接连接 AgentDock 时输入只有一个可选字段：

```json
{"workdir": "/absolute/or/host-resolvable/workspace"}
```

省略 `workdir` 使用 AgentDock 当前默认工作目录。返回结构包含：

- `workdir`：本次实际解析的工作目录；
- `workspace_root`：规则继承和 workspace Skill 扫描的根目录；
- `instructions`：按全局 → workspace root → 子目录顺序排列的规则文件状态与完整正文；
- `workspace_skills`：`<workspace>/.agents/skills/*/SKILL.md` 的 name / description / file 索引，不返回 Skill 正文；
- `warnings`：无法生成某个 best-effort 索引时的安全提示。

`instructions` 中每项包含 `scope`、`path`、`status`，并按状态提供 `content`、`sha256`、`size_bytes`、`reason` 或 `duplicate_of`。状态可能是 `loaded`、`not_found`、`empty`、`duplicate`、`skipped`、`error`。只有 `loaded` 的正文是完整有效规则；AgentDock 不会截断正文后伪装成成功加载。

## 安全与预算

每个 `AGENTS.md` 最多 64 KiB，单次 `workspace_context` 的规则正文总预算为 128 KiB。超过总预算的后续文件保留 metadata 并标记 `skipped`。读取要求 UTF-8 文本且拒绝 NUL、leaf symlink、FIFO 和其他非普通文件；使用 `os.Root`、文件身份校验和物理文件去重限制竞态与路径逃逸。

Workspace Skill 固定扫描 `<workspace>/.agents/skills/<skill-name>/SKILL.md`，只建立 metadata 索引。需要执行 Skill 时再用 `read_file` 读取返回的 `file`。选择同名能力时优先级为 workspace Skill → AgentDock Skill → `~/.agents/skills` common Skill。

Skill 路由索引按来源采用不同预算：

- `agentdock_context.skills`（standalone managed 与 Plugin-owned）返回 trim 后的完整 `description`；这两类 Skill 进入系统时已受 1024 个 Unicode 字符的规范上限约束。
- `workspace_context.workspace_skills` 同样返回 trim 后的完整 `description`，但目录仍只返回稳定排序后的前 50 项。
- `agentdock_context.common_skills` 面向数量不可控的低优先级 `~/.agents/skills`，继续保留最多 50 项、每项 `description` 最多 120 bytes 的紧凑索引预算。

## MCP 初始化

MCP 初始化 instructions 只包含稳定的 AgentDock 使用说明，不注入任何全局或工作区 `AGENTS.md` 正文。这样同一 Core 切换项目时不会在初始化上下文里残留旧工作区规则。
