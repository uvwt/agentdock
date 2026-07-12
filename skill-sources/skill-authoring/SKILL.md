---
name: skill-authoring
description: 创建、设计、修改、升级、重构和验证 AgentDock Skill 时使用；负责 Skill 边界、文档、引用、辅助脚本、测试、版本和本地安装验证。
version: 1.0.0
---

# Skill Authoring

用于创建或维护 AgentDock Skill。Skill 的本体是模型可读取的说明文档；工具负责真实检查、编辑、命令执行、打包、安装和验证。

## 何时使用

使用本 Skill 处理：

- 创建一个新的 Skill；
- 修改、重构或升级现有 Skill；
- 调整触发描述、正文流程、引用资料或辅助脚本；
- 补充 Skill 测试、示例或安全约束；
- 递增版本并完成源码侧验证和本地安装验证。

不要使用本 Skill 处理第三方 Skill 的正式安全审查、外部包安装、真实凭据配置、已安装版本回滚。这些属于 `skill-installation`。

## 核心原则

1. 先定义模型何时应该选择该 Skill，再写正文。
2. Skill 只描述方法、边界和工具选择，不承担统一执行职责。
3. 简单 Skill 优先只有一份 `SKILL.md`；只有确有需要时才增加引用、脚本或测试。
4. 修改正文、引用、脚本或行为后必须递增语义化版本。
5. 同名同版本内容必须保持不可变。
6. 环境变量、设备状态和运行数据不得进入 Skill 包。
7. 所有验证都要落到当前已安装并激活的版本，不能只看源码目录。

完整包规范见 `skill://skill-authoring/references/skill-package-spec.md`。

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

- 适用场景；
- 不适用场景；
- 读取或修改的对象；
- 默认只读行为；
- 写操作和破坏性操作的确认规则；
- 失败时需要返回的证据。

一个 Skill 应围绕一个稳定能力边界组织。发现需求已经跨越独立职责时，拆成多个 Skill，而不是继续扩大正文。

### 3. 创建源码目录

默认放在仓库的：

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

当前 AgentDock 只正式解析：

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
- 不自行增加当前解析器未支持的环境变量或执行字段。

### 5. 组织正文

正文优先包含：

1. 何时使用；
2. 不负责什么；
3. 核心原则；
4. 完整工作流程；
5. 环境变量；
6. 安全和确认规则；
7. 验证方法；
8. 失败输出要求；
9. 必要引用路径。

正文应教模型如何选择和使用真实工具，不要把 Skill 写成一组不可解释的固定命令。

### 6. 声明环境变量

每个需要配置的 Skill 都必须在正文中明确声明变量。使用统一章节：

```markdown
## 环境变量

| 变量 | 类型 | 必填 | 说明 |
|---|---|---:|---|
| EXAMPLE_BASE_URL | config | 是 | 服务地址 |
| EXAMPLE_API_KEY | secret | 是 | API Key |
```

类型至少区分 `config` 和 `secret`。还要说明缺失变量时哪些能力不可用、状态检查如何报告缺失。

环境值统一保存在：

```text
~/.agentdock/env/skill/<skill-name>.env
```

Skill 包内只声明变量名和用途，不保存真实值。

### 7. 编写引用资料

适合放入 `references/` 的内容包括：

- 稳定协议说明；
- 较长的检查清单；
- API 字段或错误码表；
- 不需要每次都完整加载的背景资料。

`SKILL.md` 必须保留足够的最小流程，不能把核心职责全部藏进引用。引用路径使用当前包内 `skill://<name>/...` 路径，不能依赖另一个 Skill 的安装目录。

### 8. 编写辅助脚本

辅助脚本只是可选执行资源，不是 Skill 本体。需要脚本时：

- 输入使用 stdin JSON 对象；
- 顶层动作字段统一使用 `skill_action`；
- 密钥只从 AgentDock 注入的环境变量读取；
- 不通过命令行参数传递秘密；
- 输出结构化 JSON；
- 错误返回明确的 `code` 和 `message`；
- 状态检查优先只读；
- 写操作遵守用户确认规则；
- 日志和错误不得回显秘密。

推荐输入：

```json
{
  "skill_action": "status"
}
```

使用 `exec_command` 直接运行辅助脚本，并通过 `skill_env` 加载该 Skill 的独立环境。

### 9. 编写测试

测试覆盖真实风险，至少考虑：

- Frontmatter 和正文可解析；
- 输入不是 JSON 对象时明确失败；
- 未知 `skill_action` 明确失败；
- 缺失环境变量时只报告变量名，不泄露值；
- 只读状态检查不产生写入；
- 破坏性动作缺少确认时拒绝；
- 日志和错误不包含秘密；
- 平台或依赖缺失时返回可诊断信息。

### 10. 安全和质量检查

提交前检查：

- 包内没有真实密码、Token、Cookie、认证缓存或会话文件；
- 没有 `.env`、缓存、数据库、截图、下载文件和运行结果；
- 没有 `__pycache__`、`*.pyc`、`node_modules` 或编译产物；
- 没有固定到某个用户的绝对路径；
- 没有符号链接或路径逃逸；
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
- 统一 Skill 执行器或旧 Skill Runtime 设计。

设备私有状态应放在：

```text
~/.agentdock/skill-data/<skill-name>/
```

环境变量单独放在：

```text
~/.agentdock/env/skill/<skill-name>.env
```

### 11. 递增版本

版本规则：

- 只修正文错字且不影响行为判断，可递增补丁版本；
- 新增兼容能力、变量、动作或引用，递增次版本；
- 改变职责边界、移除既有能力或引入不兼容流程，递增主版本。

安装前先比较当前已安装版本和新源码。不得用相同版本覆盖不同内容。

### 12. 验证和本地安装

至少完成：

1. 检查目录和 Frontmatter；
2. 检查版本是否正确递增；
3. 检查包内禁止文件和符号链接；
4. 搜索硬编码秘密；
5. 搜索旧式协议术语；
6. 对辅助脚本执行编译或语法检查；
7. 运行 Skill 自带测试；
8. 使用 `skill_package validate` 校验源码目录；
9. 使用 `skill_package install` 安装并激活；
10. 通过 `agentdock_context` 验证名称和描述已进入轻量索引；
11. 通过 `read_file skill://<name>/SKILL.md` 验证当前激活正文；
12. 有引用时读取至少一份引用；
13. 有辅助脚本时对当前激活版本运行只读 `status`。

本地安装验证只证明作者产物在当前环境可用。外部来源的正式审查、配置、更新和回滚仍交给 `skill-installation`。

## 环境变量

本 Skill 自身不需要环境变量。它要求被编写的目标 Skill 在正文中声明自己的变量需求。

## 完成标准

只有同时满足以下条件才算完成：

- 描述可稳定触发且边界清楚；
- 源码目录只包含必要文件；
- 版本与内容一致且不可变；
- 安全检查、测试和 `skill_package validate` 通过；
- 新版本已安装并激活；
- `agentdock_context` 和 `skill://` 读取的是新版本；
- 没有秘密、设备私有状态或旧式架构回流。
