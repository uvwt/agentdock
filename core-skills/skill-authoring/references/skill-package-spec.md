# AgentDock Skill 包规范

本规范供 `skill-authoring` 创建或升级 Skill。AgentDock 采用文档型 Agent Skills 模型，不再为 Skill 建立独立版本、激活或回滚体系。

## 1. 标准链路

```text
宿主发现候选 Skill 与来源
→ 返回 name / description / source_type / skill_ref / file
→ 模型选择一个精确候选
→ read_file 读取宿主返回的 file
→ 模型理解流程和约束
→ exec_command 等真实工具使用同一 skill_ref
```

Skill 负责说明“应该怎样做”；工具负责真实执行。

## 2. 最小包

```text
<skill-name>/
└── SKILL.md
```

按需扩展：

```text
<skill-name>/
├── SKILL.md
├── references/
├── scripts/
├── run.py
└── tests/
```

禁止空目录、symlink、`.env`、运行缓存和宿主安装回执。

## 3. SKILL.md

必填：

```yaml
---
name: example-skill
description: 清楚说明何时使用、解决什么问题
---

# Example Skill
```

第三方或 Agent Skills frontmatter 可以继续存在，例如：

```yaml
license: Apache-2.0
compatibility: Requires Python 3.11 or later.
metadata:
  owner: example-team
allowed-tools: exec_command
```

AgentDock Core 只对自己真正依赖的字段做约束：

- `name` 长度 1–64，只允许小写 ASCII 字母、数字和 `-`，不能以 `-` 开头/结尾，也不能包含连续 `--`；
- 目录身份与 `name` 一致；
- `description` 非空且最长 1024 个字符；
- Markdown 正文非空；
- 其他 frontmatter 字段不进入 AgentDock 强类型运行契约；它们会随原始 `SKILL.md` 保留，即使字段形状来自第三方规范；
- AgentDock 不读取任何 version 字段来选择安装内容，也不存在 active version。

## 4. 内容身份

AgentDock managed Skill 的事实目录：

```text
~/.agentdock/skills/<name>/
```

每个 managed Skill 只有当前内容。

`content_digest` 是包内容摘要，用于：

- 判断重复安装；
- 判断内容是否变化；
- 审计和验证；
- 安装事务 no-op。

它不是产品版本号，也不产生版本选择 UI/API。

相同 `content_digest` 重复安装应为 no-op；同名但 digest 不同的内容通过事务替换当前目录。

## 5. 可移植核心

目标 Skill 默认只依赖：

- `SKILL.md`；
- 包内相对路径；
- 当前进程环境；
- 宿主提供的通用工具能力。

不要在核心契约中依赖：

- `~/.agentdock/skills/<name>`；
- `AGENTDOCK_HOME` 或其他私有目录；
- `skill_ref`、`exec_command`、`skill://`；
- 固定用户绝对路径；
- 主动读取宿主私有环境文件。

AgentDock 适配说明必须可独立删除而不破坏核心流程。

## 6. 环境和数据

Skill 包只声明环境变量名称、类型、必填性、用途和缺失行为，不保存真实值。

AgentDock standalone managed Skill 环境与持久数据：

```text
~/.agentdock/env/skill/<name>.env
~/.agentdock/data/skills/<name>/
```

Plugin-owned Skill 使用独立组件环境与数据：

```text
~/.agentdock/env/skill/plugin/<plugin>/<skill>.env
~/.agentdock/data/skills/.plugin/<plugin>/<skill>/
```

Plugin-owned Skill 另外可获得 Plugin 共享兼容目录：

```text
PLUGIN_DATA_DIR=~/.agentdock/data/plugins/<plugin>/
```

standalone managed 与 Plugin-owned Skill 通过 `exec_command` 运行时，AgentDock 都把各自独立组件目录作为保留变量 `SKILL_DATA_DIR` 注入子进程。Plugin-owned Skill 另外注入 `PLUGIN_DATA_DIR`。目录按私有权限创建；Windows 原生命令得到 Host 路径，WSL 得到转换后的 Linux 路径。这两个运行时变量都不能由 Skill 环境、宿主变量映射或请求级 `env` 覆盖。

shared/workspace 候选不会得到 `SKILL_DATA_DIR` 或 `PLUGIN_DATA_DIR`。

环境和数据均独立于包内容。普通更新、remove 不删除；只有显式 purge 才删除。目标 Skill 不应硬编码 `~/.agentdock/data/...`，而应把 `SKILL_DATA_DIR` 作为 AgentDock 可选适配。

## 7. skill_ref 与来源

AgentDock 至少发现：

- `managed`
- `shared`
- `workspace`

同名不同来源不能静默覆盖。宿主返回的 `skill_ref` 是当前发现结果的精确引用，保证后续调用不会误切到同名其他来源。

`skill_ref` 不要求跨卸载、工作区迁移或重新发现长期稳定。

模型不得自己用裸名称重建来源优先级。

## 8. 并发与事务

managed Skill 的并发粒度是单个 Skill：

- read lease：读取资源和命令执行期间持有；
- write lease：安装、更新、remove 持有；
- 不使用全局粗锁。

更新流程：

```text
prepare candidate
→ validate
→ compute content_digest
→ acquire component write lock
→ compare current digest
→ same digest: no-op
→ different digest: atomic replace
→ failure: restore old current content
```

临时备份只为本次事务服务，不是历史版本库。

## 9. 禁止项

禁止：

- `agentdock.yaml`；
- `.env`；
- secret、Cookie、私钥；
- `__pycache__`、`node_modules`、数据库、下载结果；
- symlink、特殊文件、路径逃逸；
- 固定用户绝对路径；
- `skill_run`、`skill_env_manage`、`AGENTDOCK_OPERATION`、`PLUGIN_*`；
- 旧式 operation / entrypoint 清单；
- Skill 自有 version / active_version / activate / rollback / revision store。

## 10. AgentDock 管理 API

安装或更新：

```text
skill_manage
  action: install
  source: <path|zip|https>
  digest: <optional source sha256>
```

环境：

```text
skill_manage action=env_list  skill=<name>
skill_manage action=env_set   skill=<name> key=<key> value=<value>
skill_manage action=env_unset skill=<name> key=<key>
```

移除：

```text
skill_manage action=remove skill=<name>
skill_manage action=remove skill=<name> purge=true
```

没有 validate/list/inspect/update/activate/rollback/uninstall 等旧 Skill package action。发现与读取通过 context + `skill_ref` / `file` 完成。

## 11. 验证矩阵

- Frontmatter 可解析，正文非空；
- 目录身份与 `name` 一致；
- 无禁止文件与 symlink；
- lint `portable=true`；
- 脚本语法与测试通过；
- `skill_manage install` 返回有效 `content_digest`；
- 同内容重装 `changed=false`；
- 修改内容重装 `changed=true`；
- `agentdock_context` 暴露正确 provenance；
- `read_file` 使用宿主返回的 `file`；
- `exec_command` 使用同一候选 `skill_ref`；
- 更新/移除不误删环境和数据。
