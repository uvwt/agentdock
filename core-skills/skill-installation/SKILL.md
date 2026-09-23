---
name: skill-installation
description: 审查、安装、配置、验证、更新和移除 AgentDock Skill 时使用；负责来源校验、安全评估、环境配置、content digest、精确 skill_ref 与当前 managed 内容验收。
---

# Skill Installation

用于把本地或外部 Skill 安全地纳入当前 AgentDock，并验证所选来源候选确实可用。

AgentDock 的 managed Skill 只有“当前内容”，没有独立版本选择、activate 或 rollback。更新同名 managed Skill 等价于审查新内容后原子替换当前目录；相同 `content_digest` 为 no-op。

完整安全审查规范见包内 `references/skill-security-review.md`。

## 核心原则

1. 先静态审查，后安装；安装器基础校验不能替代安全审查。
2. 来源、安全风险、source digest、content digest 和缺失配置必须可追溯。
3. 默认只读；写入、删除、上传、权限变化和依赖安装必须明确识别。
4. 不替用户生成、猜测或迁移真实秘密。
5. managed Skill 环境与包内容分离；更新当前内容不得覆盖环境或持久数据。
6. 同名不同来源是不同候选，运行时只使用宿主返回的精确 `skill_ref`。
7. 不通过历史版本、activate 或 rollback 恢复旧内容；需要恢复时重新安装一个经过审查的来源快照。

## 来源与身份

安装来源可以是：

- 本地目录；
- 本地 ZIP；
- HTTPS URL。

安装前记录安全来源标签；URL 中认证信息、query 和 fragment 不写入报告或日志。

Skill 的文档身份由 `SKILL.md.name` 给出。managed 安装目标是：

```text
~/.agentdock/skills/<name>/
```

目录本身就是“已安装当前内容”的事实来源，不存在额外 active pointer 或 revision 目录。

## Frontmatter

确认根目录 `SKILL.md`：

- `name` 存在并符合命名规则；
- `description` 非空且能表达触发场景；
- 正文非空；
- `name` 遵守 1–64 字符、仅小写 ASCII 字母/数字/`-`、不首尾 `-`、不含连续 `--`；
- `description` 最长 1024 字符，`compatibility` 最长 500 字符；
- 可选 `license` 字段语义合理，`metadata` 必须是 string→string，`allowed-tools` 必须是单行空格分隔字符串；
- `metadata.version` 若存在只作为作者元数据，不参与 AgentDock 安装或运行；
- 未把 `version`、`active_version`、activate、rollback 当作 AgentDock Skill 生命周期。

发现 `agentdock.yaml`、旧统一执行协议或旧 Skill Runtime 设计时，停止安装并要求迁移。

## 静态安全审查

至少检查：

- 所有普通、隐藏、脚本、测试、依赖与锁文件；
- 网络目标、协议、上传数据和下载后执行；
- Shell/子进程和用户输入拼接；
- 文件读取、写入、覆盖、删除、路径穿越；
- 权限、启动项、计划任务和持久化；
- API Key、Token、Cookie、私钥、浏览器状态、SSH/云凭据；
- 外部依赖、安装钩子和二进制；
- 日志和异常是否泄露秘密；
- 破坏性动作是否要求用户确认；
- 符号链接与特殊文件。

风险分级：

| 等级 | 含义 | 默认处理 |
|---|---|---|
| low | 纯文档或行为明确且低风险 | 可继续 |
| medium | 有明确网络、写入或普通依赖 | 展示风险后继续 |
| high | 涉及敏感数据、广泛访问、删除或持久化 | 需明确确认 |
| blocked | 危险行为不可接受或无法解释 | 停止 |

## 环境变量

从正文提取变量名、类型、必填性与用途。

standalone managed Skill 使用：

```text
~/.agentdock/env/skill/<skill-name>.env
~/.agentdock/data/skills/<skill-name>/
```

Plugin-owned Skill 使用精确 `skill_ref` 管理独立环境和数据：

```text
~/.agentdock/env/skill/plugin/<plugin>/<skill>.env
~/.agentdock/data/skills/.plugin/<plugin>/<skill>/
```

通过 `skill_manage env_list/env_set/env_unset` 管理环境；工具不会返回真实值。

standalone managed 和 Plugin-owned Skill 执行时都会获得独立 `SKILL_DATA_DIR`。Plugin-owned Skill 另外获得共享 `PLUGIN_DATA_DIR=~/.agentdock/data/plugins/<plugin>/`。Windows 原生命令收到 Host 路径，WSL 收到已转换的 Linux 路径；两个变量都不能通过 `skill_manage env_set`、宿主 env mapping 或请求级 `env` 覆盖。

shared/workspace 候选不会收到 `SKILL_DATA_DIR` 或 `PLUGIN_DATA_DIR`，即使名称相同。

普通 remove 默认保留环境和数据；只有用户明确要求 purge 时才一起删除。内容更新也不得清空或重建该目录。

## 安装或更新

使用：

```text
skill_manage
  action: install
  source: <local path | zip | https URL>
  digest: <optional expected SHA-256 source digest>
```

安装流程应满足：

1. 来源先进入临时位置；
2. 校验预期 source digest（若提供）；
3. 校验包结构、`SKILL.md`、禁止文件与 symlink；
4. 计算 `content_digest`；
5. 与当前同名 managed 内容相同则 no-op；
6. 内容变化时在组件写锁下原子替换当前目录；
7. 提交失败时旧当前内容保持完整；
8. 环境和持久数据不随包替换。

安装结果至少核对：

- `skill`
- `content_digest`
- `changed`

不要寻找 version、Activated、active_version 或 revision history。

## 精确来源发现与运行

安装后调用 `agentdock_context`。每个 Skill 候选应包含：

- `name`
- `description`
- `source_type`
- `source_id`
- `skill_ref`
- `file`
- managed 来源可包含 `content_digest`

来源至少包括：

- `managed`
- `shared`
- `workspace`

同名候选不静默覆盖。选择一个候选后，后续读取和执行都使用它自己的 `file` / `skill_ref`。

不要把名称自行拼成路径，也不要在不同调用中重新按优先级解析。

## 安装后验收

至少验证：

1. `skill_manage install` 成功；
2. 返回 `content_digest`，必要时与审查内容核对；
3. `agentdock_context` 中出现预期来源候选；
4. 使用该候选返回的 `file` 读取 `SKILL.md`；
5. 有引用时读取至少一份引用；
6. `skill_manage env_list` 显示必填变量状态完整；
7. 有辅助脚本时用候选的 `skill_ref` 调用 `exec_command` 做只读 `status`；
8. 日志和结果不含 secret；
9. managed 环境只在 managed 候选执行时注入，不借给同名 shared/workspace Skill；
10. 若 Skill 有持久状态，managed 只读验证中确认 `SKILL_DATA_DIR` 存在且指向私有数据目录；shared/workspace 验证中确认该变量未被借用。

命令执行期间 AgentDock 会对 managed Skill 持有读锁；安装、更新和 remove 使用写锁，避免运行中目录被替换或删除。

## 更新

更新不是“安装新版本并激活”，而是重新审查一个新来源后对同名 managed Skill 执行 `install`。

更新前比较：

- 文件增删和正文变化；
- 网络、权限、依赖、环境变量和数据格式变化；
- 破坏性行为和确认规则；
- source digest / content digest；
- 来源可信度。

相同内容应返回 `changed=false`。内容变化成功提交后，新内容成为唯一当前 managed 内容。

## 恢复旧内容

AgentDock 不保留 Skill revision history，也不提供 rollback action。

需要恢复旧内容时：

1. 找到可信、可审查的旧来源快照；
2. 按完整安全流程重新审查；
3. 对该来源执行 `skill_manage install`；
4. 再次验证 `content_digest`、上下文候选与只读行为。

不要依赖 AgentDock 内部临时备份；事务备份只用于当前安装操作失败时恢复，不是用户可用版本库。

## 移除

默认：

```text
skill_manage
  action: remove
  skill: <name>
```

删除当前 managed 包，但保留独立环境与持久数据。

只有用户明确要求一并删除时：

```text
skill_manage
  action: remove
  skill: <name>
  purge: true
```

remove 不影响 shared 或 workspace 的同名候选。移除 managed 后重新调用上下文时，其他来源仍可能作为独立候选出现。

## 完成标准

- 来源与摘要已记录；
- 安全审查有风险等级与证据；
- 无 blocked 项；
- managed 安装/更新结果有 `content_digest`；
- 相同内容重复安装为 no-op；
- `agentdock_context` 的来源、`skill_ref`、`file` 正确；
- 环境状态完整且未泄露值；
- 代表性只读验证通过；
- 更新、remove 未误删环境或持久数据；
- 没有依赖 Skill 版本、activate、rollback 或 active revision。
