---
name: skill-authoring
description: 创建、设计、修改、升级、重构和验证 AgentDock Skill 时使用；负责可移植核心、文档、引用、辅助脚本、测试、版本和本地安装验证。
version: 1.1.2
---

# Skill Authoring

用于创建或维护 AgentDock 第一方 Skill。Skill 的本体是模型可读取的说明文档；工具负责真实检查、编辑、命令执行、打包、安装和验证。

目标 Skill 应由两部分构成：

```text
可移植核心契约
+
可选的宿主适配说明
```

移除 AgentDock 专属适配说明后，Skill 的业务流程、包内引用、环境变量契约和辅助脚本仍应完整可用。

## 何时使用

使用本 Skill 处理：

- 创建新的第一方 Skill；
- 修改、重构或升级现有 Skill；
- 调整触发描述、正文流程、引用资料或辅助脚本；
- 补充测试、示例、安全约束和可移植性检查；
- 递增版本并完成源码侧与当前激活版本验证。

不要使用本 Skill 处理第三方 Skill 的正式安全审查、真实凭据配置或已安装版本回滚。这些属于 `skill-installation`。

## 核心原则

1. 先定义模型何时应该选择该 Skill，再写正文。
2. Skill 只描述方法、边界和工具选择，不承担统一执行职责。
3. Skill 核心契约必须与宿主无关；包内文件使用相对路径，环境由运行宿主注入。
4. 简单 Skill 优先只有一份 `SKILL.md`；只有确有需要时才增加引用、脚本或测试。
5. 修改正文、引用、脚本或行为后必须递增语义化版本。
6. 同名同版本内容必须保持不可变。
7. 环境值、设备状态和运行数据不得进入 Skill 包。
8. 所有验证都要落到当前已安装并激活的版本，不能只看源码目录。
9. 第一方 Skill 必须通过本 Skill 的 `lint`，不能只通过包安装校验。

完整规范见包内 `references/skill-package-spec.md`。

## 标准流程

### 1. 理解需求和触发条件

先明确：

- 用户真正要解决的问题；
- 模型在什么请求下应选择该 Skill；
- 哪些相邻任务不属于该 Skill；
- 需要调用哪些真实工具；
- 是否需要辅助脚本、引用资料或测试；
- 是否涉及网络、写入、删除、凭据或高风险动作。

不要用“管理某能力全生命周期”这类宽泛描述。`description` 必须让模型能稳定判断何时选中它。

### 2. 确定职责边界

正文至少说明：

- 适用场景和不适用场景；
- 读取或修改的对象；
- 默认只读行为；
- 写操作和破坏性操作的确认规则；
- 失败时需要返回的证据。

一个 Skill 应围绕一个稳定能力边界组织。需求已经跨越独立职责时，应拆成多个 Skill。

### 3. 创建源码目录

默认放在仓库：

```text
skill-sources/<skill-name>/
```

按需选择结构：

```text
skill-sources/<skill-name>/
└── SKILL.md
```

```text
skill-sources/<skill-name>/
├── SKILL.md
├── references/
├── scripts/
└── tests/
```

```text
skill-sources/<skill-name>/
├── SKILL.md
├── run.py
└── tests/
```

不要为了形式创建空目录。

### 4. 编写 Frontmatter

当前 AgentDock 正式解析：

```yaml
---
name: example-skill
description: 清楚说明何时使用、解决什么问题
version: 1.0.0
---
```

要求：

- `name` 使用稳定、简短、全小写的连字符名称；
- `description` 同时覆盖触发场景和能力边界；
- `version` 使用语义化版本；
- Frontmatter 后必须有非空 Markdown 正文；
- 不增加当前解析器未支持的环境变量或执行字段。

### 5. 编写可移植核心

目标 Skill 的正文和脚本默认只假设：

- 当前工作目录是 Skill 包根目录；
- 包内资源可通过相对路径访问；
- 环境变量来自当前进程环境；
- 运行宿主负责选择工具、切换目录和注入环境；
- 不依赖 AgentDock 的安装目录、状态目录或专属变量。

有根目录脚本时，通用执行示例应写成：

```bash
printf '%s' '{"skill_action":"status"}' | python3 run.py
```

不得把以下内容作为核心运行前提：

- `~/.agentdock/skill-store/installed/...`；
- 固定安装版本号；
- `AGENTDOCK_HOME` 或 `AGENTDOCK_SKILL_DIR` 用于定位包内脚本；
- `skill_env`、`exec_command` 或 `skill://`；
- 固定用户绝对路径；
- 主动读取或 `source` AgentDock 私有环境文件。

AgentDock 工具调用可以出现在单独的“AgentDock 适配/验证”说明中，但删除该部分后，Skill 仍必须可用。

### 6. 声明环境变量

每个需要配置的目标 Skill 都必须在正文中明确声明变量：

```markdown
## 环境变量

| 变量 | 类型 | 必填 | 说明 |
|---|---|---:|---|
| EXAMPLE_BASE_URL | config | 是 | 服务地址 |
| EXAMPLE_API_KEY | secret | 是 | API Key |
```

类型至少区分 `config` 和 `secret`，并说明缺失变量时哪些能力不可用。

目标 Skill 只声明变量名和用途，不声明 AgentDock 私有保存路径，不保存真实值。辅助脚本只从当前进程环境读取变量。

在 AgentDock 本地验证时，环境值由 `skill_package env_set/env_unset/env_list` 管理，并由 `exec_command` 的 `skill` 上下文注入本次子进程。环境不会写入 AgentDock 主进程或系统环境。

### 7. 编写引用资料

适合放入 `references/` 的内容包括：

- 稳定协议说明；
- 较长检查清单；
- API 字段或错误码表；
- 不需要每次完整加载的背景资料。

目标 `SKILL.md` 使用包内相对路径，例如：

```text
references/api.md
```

不要把 `skill://<name>/...` 写成核心引用契约，也不要依赖另一个 Skill 的安装目录。AgentDock 在验证当前激活包时可通过 `read_file skill://<name>/...` 读取资源。

### 8. 编写辅助脚本

辅助脚本只是可选资源，不是 Skill 本体。需要脚本时：

- 输入使用 stdin JSON 对象；
- 顶层动作字段统一使用 `skill_action`；
- 密钥只从当前进程环境读取；
- 不通过命令行参数传递秘密；
- 输出结构化 JSON；
- 错误返回稳定 `code` 和可读 `message`；
- 状态检查优先只读；
- 写操作遵守用户确认规则；
- 日志和错误不得回显秘密；
- 包内文件使用相对路径或基于脚本自身目录定位。

推荐输入：

```json
{
  "skill_action": "status"
}
```

通用执行在 Skill 包根目录运行：

```bash
python3 run.py
```

AgentDock 验证时使用 `exec_command` 的 `skill: "<skill-name>"` 绑定当前激活目录与独立环境，不手工解析版本目录。

### 9. 运行可移植性 lint

本 Skill 的 `run.py` 提供：

- `status`：报告 lint 版本和规则数量；
- `lint`：检查目标 Skill 的可移植核心和宿主绑定问题。

输入示例：

```json
{
  "skill_action": "lint",
  "source": "/path/to/skill-sources/example-skill"
}
```

结果包含：

- `portable`；
- `error_count`；
- `warning_count`；
- 每条 issue 的 `code`、`severity`、文件、行号、说明和修复建议。

硬编码已安装版本目录、依赖 AgentDock 专属目录变量、主动读取 AgentDock 环境文件和固定用户绝对路径属于 error。AgentDock 专属工具或 URI 出现在目标 `SKILL.md` 中属于 warning，需要确认它们只存在于可选适配说明。

对已安装目录运行 lint 时，会忽略包根目录下由 AgentDock 安装器生成的 `.agentdock-install.json`。该文件属于宿主安装回执，不是 Skill 包内容；同名文件出现在包内其他目录时仍会正常扫描。

在 AgentDock 中，安装新版 `skill-authoring` 后可这样运行：

```text
exec_command
  skill: skill-authoring
  cmd: python3 run.py
  stdin: {"skill_action":"lint","source":"/path/to/source"}
```

创建或修改第一方 Skill 时，`portable` 必须为 `true`；warning 必须逐项修复或说明为什么属于可选宿主适配。

### 10. 编写测试

测试覆盖真实风险，至少考虑：

- Frontmatter 和正文可解析；
- 输入不是 JSON 对象时明确失败；
- 未知 `skill_action` 明确失败；
- 缺失环境变量时只报告变量名，不泄露值；
- 只读状态检查不产生写入；
- 破坏性动作缺少确认时拒绝；
- 日志和错误不包含秘密；
- 平台或依赖缺失时返回可诊断信息；
- 包从根目录使用相对命令运行；
- 脚本不依赖 AgentDock 私有目录。

### 11. 安全和质量检查

提交前检查：

- 包内没有真实密码、Token、Cookie、认证缓存或会话文件；
- 没有 `.env`、缓存、数据库、截图、下载文件和运行结果；
- 没有 `__pycache__`、`*.pyc`、`node_modules` 或编译产物；
- 没有固定用户绝对路径、符号链接或路径逃逸；
- 没有旧式统一执行协议、旧式环境工具示例或旧清单；
- 网络目标、文件写入、删除、上传和权限变化均被明确说明；
- 依赖安装不会被隐藏执行。

明确禁止生成或恢复：

- `agentdock.yaml`；
- `skill_run`；
- `skill_env_manage`；
- `AGENTDOCK_OPERATION`；
- `PLUGIN_*` 旧协议；
- 旧式 `operation` 或 `entrypoint` 清单；
- 统一 Skill 执行器或旧 Skill Runtime。

设备私有状态和 AgentDock 环境值只属于宿主，不进入包。

### 12. 递增版本

- 只修正文错字且不影响行为判断，可递增补丁版本；
- 新增兼容能力、变量、动作或引用，递增次版本；
- 改变职责边界、移除既有能力或引入不兼容流程，递增主版本。

安装前比较当前已安装版本和新源码。不得用相同版本覆盖不同内容。

### 13. 验证和本地安装

至少完成：

1. 检查目录和 Frontmatter；
2. 检查版本正确递增；
3. 检查禁止文件、符号链接和真实 secret；
4. 对辅助脚本执行语法检查和自带测试；
5. 运行 `skill-authoring lint`，确认 `portable=true` 并审查 warning；
6. 使用 `skill_package validate` 校验包级合法性；
7. 使用 `skill_package install` 安装并激活；
8. 通过 `agentdock_context` 验证名称和描述进入轻量索引；
9. 通过 `read_file skill://<name>/SKILL.md` 验证当前激活正文；
10. 有引用时读取至少一份引用；
11. 有辅助脚本时，用 `exec_command skill=<name>` 对当前激活版本运行只读 `status`；
12. 运行一个代表性低风险动作，无法运行时记录真实原因。

`skill_package validate` 负责包能否合法安装；本 Skill 的 `lint` 负责第一方创作质量和可移植性，两者不能互相替代。

## 环境变量

本 Skill 自身不需要环境变量。它要求被编写的目标 Skill 声明业务变量，并由运行宿主注入当前子进程。

## 完成标准

只有同时满足以下条件才算完成：

- 描述可稳定触发且边界清楚；
- 可移植核心不依赖 AgentDock 私有目录或工具参数；
- 包内脚本可从 Skill 根目录使用相对路径运行；
- `skill-authoring lint` 返回 `portable=true`，warning 已逐项处理；
- 安全检查、测试和 `skill_package validate` 通过；
- 新版本已安装并激活；
- `agentdock_context` 和 `skill://` 读取的是新版本；
- 当前激活版本的真实只读验证通过；
- 没有秘密、设备私有状态或旧式架构回流。
