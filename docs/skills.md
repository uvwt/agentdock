# Skill 设计与运行模型

AgentDock 的 Skill 是给模型读取的工作方法和能力契约，不是插件进程、统一执行器或固定入口。模型先读取说明、理解边界，再选择命令、文件、浏览器或 MCP 等真实工具完成任务。

## 设计思想

第一方 Skill 采用两层结构：

```text
可移植核心契约
+
可选宿主适配
```

### 可移植核心

可移植核心描述业务流程本身，并且不依赖 AgentDock 私有实现：

- 包内文件使用相对路径；
- 配置和凭据只从当前进程环境读取；
- 辅助脚本可以从 Skill 包根目录直接运行；
- 输入输出使用普通 JSON、文本或标准协议；
- 只声明需要什么能力，不绑定某个宿主的工具参数。

删除 AgentDock 适配说明后，Skill 的业务流程、引用、环境变量契约和辅助脚本仍应在其他兼容宿主中使用。

可移植核心不得依赖：

- `~/.agentdock/skill-store/installed/...` 或固定安装版本；
- `AGENTDOCK_HOME`、`AGENTDOCK_SKILL_DIR` 等宿主目录变量；
- 手工读取 Skill state 或拼接激活版本路径；
- 主动读取或 `source` AgentDock 私有环境文件；
- `skill_env`、`exec_command`、`skill://` 等 AgentDock 专属接口；
- 固定用户绝对路径。

### AgentDock 宿主适配

AgentDock 只负责把可移植核心安全、稳定地接到当前设备：

- `agentdock_context` 提供轻量 Skill 索引；
- `read_file skill://<name>/SKILL.md` 读取当前激活版本；
- `skill_package` 负责校验、安装、回滚和独立环境管理；
- `exec_command skill=<name>` 负责本次命令的目录和环境上下文；
- 文件、浏览器和 MCP 工具负责真实动作。

宿主适配可以出现在独立章节中，但不能反过来成为 Skill 核心运行前提。

## 为什么采用这种模型

这一分层解决了几个长期问题：

1. **可移植**：Skill 不会因为换宿主、换设备或换安装目录而失效。
2. **版本稳定**：模型不需要读取 state 或硬编码版本，宿主统一解析当前激活版本。
3. **环境隔离**：凭据只进入本次子进程，不污染 AgentDock 主进程或系统环境。
4. **职责清晰**：Skill 教模型怎么做，Tool 提供真实能力，包管理只负责生命周期。
5. **避免黑盒化**：不恢复统一 Skill 执行器，不把模型可理解的流程重新封装成不可见动作。
6. **便于审查**：创作质量、包合法性、安装状态和真实行为可以分别验证。

## 包结构

最小 Skill：

```text
skill-sources/example-skill/
└── SKILL.md
```

按需增加：

```text
skill-sources/example-skill/
├── SKILL.md
├── references/
├── scripts/
├── run.py
└── tests/
```

`SKILL.md` Frontmatter 只使用：

```yaml
---
name: example-skill
description: 说明何时使用、解决什么问题以及能力边界
version: 1.0.0
---
```

辅助脚本不是统一 entrypoint。存在 `run.py` 时，通用执行示例应从包根目录使用相对路径：

```bash
printf '%s' '{"skill_action":"status"}' | python3 run.py
```

## AgentDock 执行语义

在 AgentDock 中运行当前激活 Skill 的辅助脚本：

```text
exec_command
  skill: example-skill
  cmd: python3 run.py
  stdin: {"skill_action":"status"}
```

`skill` 上下文只作用于当前命令或命令 session：

1. 解析该 Skill 的当前激活版本；
2. 未显式传入 `workdir` 时，将工作目录设为激活包根目录；
3. 加载 `~/.agentdock/env/skill/<name>.env`；
4. 再应用本次显式 `env` 覆盖；
5. 启动子进程；
6. 命令或 session 结束后，注入环境随子进程一起消失。

优先级：

```text
显式 workdir > Skill 根目录 > AgentDock 默认工作目录
显式 env > Skill 独立环境 > AgentDock 进程环境
```

旧 `skill_env` 只保留“注入环境但不切换目录”的兼容语义。新 Skill 不应把它写入核心契约。`skill` 与 `skill_env` 同时出现但指向不同 Skill 时，AgentDock 会拒绝执行。

## 环境和秘密

目标 Skill 只在正文中声明变量名、类型、是否必填、用途和缺失影响，例如：

```markdown
| 变量 | 类型 | 必填 | 说明 |
|---|---|---:|---|
| EXAMPLE_BASE_URL | config | 是 | 服务地址 |
| EXAMPLE_API_KEY | secret | 是 | API Key |
```

真实值通过 `skill_package env_set/env_unset/env_list` 管理：

- `env_list` 只返回变量名和是否已配置；
- 环境文件不进入 Skill 包；
- 脚本只读取当前进程环境；
- Token、Cookie、密码、私钥、session 和缓存不得提交到仓库或 Recall。

## 校验职责

两个校验入口不能互相替代：

### `skill-authoring lint`

负责第一方 Skill 的创作质量和可移植性，例如：

- 是否硬编码安装版本目录；
- 是否依赖 AgentDock 私有目录变量；
- 是否主动读取宿主环境文件；
- 是否出现固定用户路径；
- AgentDock 专属接口是否错误进入核心契约；
- 存在辅助脚本时是否说明相对执行方式。

第一方 Skill 要求 `portable=true`；warning 必须修复，或确认只存在于可删除的宿主适配说明中。

### `skill_package validate`

负责包能否合法安装，例如：

- `SKILL.md` 和 Frontmatter 是否有效；
- 包结构、大小和路径是否合法；
- 是否包含禁止文件、符号链接或危险产物；
- 同名同版本内容是否保持不可变。

它不承担第一方创作风格和可移植性判断。

## 创建和更新流程

1. 明确触发条件、职责边界、默认只读行为和真实工具。
2. 编写可移植核心，再补充可选 AgentDock 适配说明。
3. 对脚本运行语法检查和包内测试。
4. 运行 `skill-authoring lint`。
5. 运行 `skill_package validate`。
6. 递增语义化版本并安装激活。
7. 通过 `agentdock_context` 和 `skill://` 验证当前激活正文。
8. 使用 `exec_command skill=<name>` 对当前激活版本运行只读状态和代表性低风险动作。

不得恢复 `agentdock.yaml`、manifest operation/entrypoint、`skill_run`、`skill_env_manage`、统一 Skill 执行器或旧 Skill Runtime。
