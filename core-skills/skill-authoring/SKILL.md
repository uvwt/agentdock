---
name: skill-authoring
description: 创建、设计、修改、重构和验证 AgentDock Skill 时使用；负责 Agent Skills 兼容的 SKILL.md、可移植核心、引用、辅助脚本、测试、安全边界和本地真实验证。
---

# Skill Authoring

用于创建或维护 AgentDock Skill。Skill 的本体是模型可读取的说明文档；真实操作仍由命令、文件、浏览器、MCP 等工具完成。

AgentDock 不为 Skill 建立独立版本生命周期。Skill 的当前内容由来源与内容摘要识别；同名 managed Skill 更新时直接原子替换当前内容，不保留可选历史 revision，不提供 activate 或 rollback。

## 核心原则

1. 先定义模型何时应该选择该 Skill，再写正文。
2. Skill 核心应兼容 Agent Skills 的文档模型，不把 AgentDock 私有运行时当作通用契约。
3. 最小 Skill 只有一个 `SKILL.md`；只有确有需要时才增加 `references/`、脚本或测试。
4. 包内资源使用相对路径，业务环境变量由宿主注入当前进程。
5. 秘密、缓存、会话、数据库、下载结果和设备私有状态不得进入 Skill 包。
6. 修改后必须真实 lint、测试，并在需要时安装到 AgentDock 验证当前内容。
7. 不创建或恢复 Skill 自有版本选择、激活、回滚或历史 revision 机制。

完整规范见 `references/skill-package-spec.md`。

## 目录

普通第一方和社区 Skill 默认放在独立 Skill 仓库：

```text
skills/<skill-name>/
└── SKILL.md
```

按需扩展：

```text
skills/<skill-name>/
├── SKILL.md
├── references/
├── scripts/
├── run.py
└── tests/
```

只有必须随 AgentDock runtime 一起交付的核心 Skill 放在：

```text
core-skills/<skill-name>/
```

不要创建空目录。

## SKILL.md Frontmatter

AgentDock 使用 Agent Skills 风格的字段：

```yaml
---
name: example-skill
description: 清楚说明何时使用、解决什么问题
license: Apache-2.0
compatibility: Requires Python 3.11 or later.
metadata:
  owner: example-team
allowed-tools: exec_command
---

# Example Skill
```

AgentDock 真正依赖并严格校验的只有：

- `name` 必填，长度 1–64，只允许小写 ASCII 字母、数字和 `-`，不能以 `-` 开头/结尾，也不能包含连续 `--`；
- `description` 必填，最长 1024 个 Unicode 字符，并能让模型稳定判断何时使用；重要的 Use when、Do not use 和相邻 Skill 边界条件不要依赖正文补充，因为模型会先用 description 做候选路由；
- Markdown 正文必须非空；
- 其他 frontmatter（包括 `license`、`compatibility`、`metadata`、`allowed-tools`、`version` 以及第三方扩展字段）由作者生态定义，AgentDock 原样保留但不作为安装/运行前提；
- 不设计 AgentDock 私有的 `version`、`active_version`、revision 或 rollback 契约。

## AgentDock 路由索引

AgentDock 会先暴露轻量 description 索引，再按需读取完整 `SKILL.md`。不同来源的索引预算不同：

- standalone managed 与 Plugin-owned Skill：`agentdock_context.skills` 返回 trim 后的完整 `description`；
- workspace Skill：`workspace_context.workspace_skills` 返回 trim 后的完整 `description`，但最多列出 50 项；
- shared/common Skill：`agentdock_context.common_skills` 面向数量不可控的 `~/.agents/skills`，最多列出 50 项，并把每项 `description` 限制在 120 bytes。

因此 authoring 时应把 description 视为路由契约，而不是正文摘要的随意前缀。AgentDock 不提供可配置的 description 截断上限；如果未来 Skill 总量显著增长，应通过索引总预算或检索式路由解决，而不是静默裁掉每个已管理 Skill 的 description 后半段。

## 可移植核心

目标 Skill 默认只假设：

- 当前进程能读取 Skill 包内容；
- 包内资源使用相对路径；
- 环境变量来自当前进程；
- 宿主提供命令、文件、浏览器或远端工具能力。

有根目录脚本时，通用示例写成：

```bash
printf '%s' '{"skill_action":"status"}' | python3 run.py
```

不要把以下内容作为核心运行前提：

- `~/.agentdock/skills/<name>`；
- `AGENTDOCK_HOME`、`AGENTDOCK_SKILL_DIR` 等 AgentDock 私有路径变量；
- `skill_ref`、`exec_command` 或 `skill://`；
- 固定用户绝对路径；
- 主动读取或 source AgentDock 私有环境文件。

AgentDock 专属说明可以放在独立的“AgentDock 适配/验证”章节；删除该章节后，核心流程仍应成立。

## 环境变量

需要配置时，在正文中明确声明变量名、类型、必填性和用途：

```markdown
## 环境变量

| 变量 | 类型 | 必填 | 说明 |
|---|---|---:|---|
| EXAMPLE_BASE_URL | config | 是 | 服务地址 |
| EXAMPLE_API_KEY | secret | 是 | API Key |
```

Skill 只声明变量，不保存真实值。辅助脚本只从当前进程环境读取。

AgentDock 本地验证时：

- `skill_manage env_list` 查看变量名称与配置状态；
- `skill_manage env_set` 写入用户明确提供的值；
- `skill_manage env_unset` 删除指定变量；
- `exec_command skill_ref=<host-issued-ref>` 运行时只把 managed Skill 环境注入对应子进程。

环境值不写入 AgentDock 主进程或系统全局环境。

需要持久可变状态时，不要写 Skill 包目录。AgentDock 对 **standalone managed** 与 **Plugin-owned** Skill 的 `exec_command` 都会提供独立运行时保留变量 `SKILL_DATA_DIR`：

- standalone managed 指向 `~/.agentdock/data/skills/<name>/`；
- Plugin-owned 指向 `~/.agentdock/data/skills/.plugin/<plugin>/<skill>/`，并额外获得 Plugin 共享兼容目录 `PLUGIN_DATA_DIR=~/.agentdock/data/plugins/<plugin>/`；
- 数据目录只在对应 Skill / Plugin 运行时真正需要时创建；
- Unix 权限收紧为 `0700`，Windows 使用当前用户私有 ACL；
- Windows 原生命令收到 Host 路径；WSL 命令收到已转换的 Linux 路径；
- `skill_manage env_set`、宿主 env mapping 和 `request.env` 都不能覆盖运行时保留变量；
- Plugin-owned Skill 的用户环境隔离在 `~/.agentdock/env/skill/plugin/<plugin>/<skill>.env`；standalone 仍使用 `~/.agentdock/env/skill/<name>.env`；
- shared/workspace 候选不会得到 `SKILL_DATA_DIR` 或 `PLUGIN_DATA_DIR`。

`SKILL_DATA_DIR` / `PLUGIN_DATA_DIR` 是 AgentDock 可选适配，不是 Agent Skills 通用前提。可移植 Skill 不应把它们列为用户必填配置；需要状态目录的辅助脚本可以在检测到它们时优先使用，并在其他宿主下采用自己明确声明的可移植策略。

## 引用与辅助脚本

正文引用包内文件时使用相对路径，例如：

```text
references/api.md
```

辅助脚本应：

- 通过 stdin 接收 JSON 对象；
- 顶层动作字段使用 `skill_action`；
- secret 只从环境读取；
- 输出结构化 JSON；
- 错误返回稳定 `code` 和可读 `message`；
- 默认状态检查只读；
- 破坏性动作要求明确确认；
- 不回显 secret；
- 用相对路径或脚本目录定位包内只读资源；
- 可变缓存、数据库、会话和下载结果不得写回 Skill 包；在 AgentDock managed 运行时优先写入 `SKILL_DATA_DIR`。

## Authoring lint

本 Skill 的 `run.py` 提供：

- `status`：返回 `lint_version` 与规则数量；
- `lint`：检查可移植性和明显宿主绑定。

示例：

```json
{
  "skill_action": "lint",
  "source": "/path/to/skills/example-skill"
}
```

硬错误包括：

- 硬编码 AgentDock managed Skill 安装目录；
- 依赖 AgentDock 私有目录变量；
- 主动读取 AgentDock 环境文件；
- 固定用户绝对路径。

AgentDock 专属 `skill_ref`、`exec_command`、`skill://` 出现在目标 `SKILL.md` 中会作为 warning，要求确认它们只存在于可选宿主适配说明。

## 安全与质量检查

提交前至少检查：

- 无真实密码、Token、Cookie、私钥和认证缓存；
- 无 `.env`、数据库、截图、下载结果、`__pycache__`、`node_modules` 等运行产物；
- 无符号链接、路径逃逸、固定用户绝对路径；
- 网络、写入、删除、上传、权限变化和依赖安装均有明确说明；
- 未恢复 `agentdock.yaml`、`skill_run`、`skill_env_manage`、旧式 operation/entrypoint 清单或统一 Skill Runtime；
- 不把安装历史、激活版本或回滚机制重新写回 Skill。

## AgentDock 本地验证

AgentDock 的 managed Skill 当前布局是：

```text
~/.agentdock/skills/<name>/
  SKILL.md
  ...
```

这个目录是宿主实现细节，不应硬编码进目标 Skill。

建议验证流程：

1. 检查目录、Frontmatter 和正文；
2. 检查禁止文件、符号链接和真实 secret；
3. 对辅助脚本做语法检查和自带测试；
4. 运行 `skill-authoring lint`，确认 `portable=true`；
5. 使用 `skill_manage install` 从本地目录、压缩包或 HTTPS 来源安装；
6. 记录返回的 `content_digest`；相同内容重复安装应为 no-op；
7. 通过 `agentdock_context` 找到该候选，并保存它返回的 `skill_ref` 与 `file`；
8. 用 `read_file` 读取宿主返回的 `file`，不要自己按名称重建 URI；
9. 有辅助脚本时，用同一候选的 `skill_ref` 调用 `exec_command` 做只读检查；
10. 修改内容后再次安装，确认当前内容被原子替换且环境/数据未被清空。

managed、shared、workspace 中同名 Skill 是不同候选。验证时始终使用宿主返回的精确 `skill_ref`，不得靠裸名称重新解析。

## 完成标准

只有同时满足以下条件才算完成：

- description 可稳定触发且职责边界清楚；
- Frontmatter 与 Agent Skills 文档模型兼容；
- 可移植核心不依赖 AgentDock 私有目录或工具参数；
- 包内脚本可从 Skill 根目录按相对路径运行；
- lint、测试和安全检查通过；
- managed 安装时有有效 `content_digest`，重复相同内容为 no-op；
- `agentdock_context` 返回正确来源、`skill_ref` 与 `file`；
- 当前内容和代表性只读行为已真实验证；
- 没有 Skill 自有版本选择、激活、回滚或历史 revision 语义。
