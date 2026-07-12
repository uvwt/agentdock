# AgentDock Skill 包规范

本规范供 `skill-authoring` 在创建或升级 Skill 时使用。以当前 AgentDock 纯文档架构为准。

## 1. 架构边界

标准调用链：

```text
agentdock_context
→ read_file skill://<name>/SKILL.md
→ 模型理解流程和约束
→ exec_command / file_edit / skill_package / 浏览器 / MCP 等工具执行
```

Skill 负责说明“应该怎样做”；工具负责真实执行。包内脚本只是模型可选择调用的资源。

## 2. 最小包结构

最简单的 Skill：

```text
skill-sources/<skill-name>/
└── SKILL.md
```

需要扩展资源时：

```text
skill-sources/<skill-name>/
├── SKILL.md
├── references/
├── scripts/
└── tests/
```

可将单一入口脚本直接放在包根目录，例如 `run.py`。不要创建空目录。

## 3. SKILL.md Schema

当前必填 Frontmatter：

```yaml
---
name: example-skill
description: 清楚说明何时使用、解决什么问题
version: 1.0.0
---
```

规则：

- `name` 匹配 `^[a-z][a-z0-9-]{1,62}$`；
- `description` 非空，并能支持模型选择；
- `version` 是语义化版本，可带 `v` 前缀和预发布或构建后缀；
- Markdown 正文非空；
- 当前解析器只读取 `name`、`description`、`version`；
- 不把未经支持的字段当作正式契约。

## 4. 版本和不可变性

任何会改变模型判断或执行行为的内容都需要递增版本，包括：

- `SKILL.md` 正文；
- Frontmatter 描述；
- `references/`；
- 辅助脚本；
- 测试所代表的行为契约；
- 依赖和平台要求。

同名同版本安装包内容必须相同。AgentDock 会拒绝用不同内容覆盖已安装的同一版本。

建议：

- 修复兼容性问题：补丁版本；
- 新增向后兼容能力：次版本；
- 改变职责边界或不兼容行为：主版本。

## 5. 环境变量契约

需要配置时，在正文中声明：

```markdown
## 环境变量

| 变量 | 类型 | 必填 | 说明 |
|---|---|---:|---|
| EXAMPLE_BASE_URL | config | 是 | 服务地址 |
| EXAMPLE_API_KEY | secret | 是 | API Key |
```

值保存在：

```text
~/.agentdock/env/skill/<skill-name>.env
```

目录权限应为 `0700`，文件权限应为 `0600`。Skill 包只包含变量名称、类型、必填性、用途和缺失行为，不包含真实值。

当前通过 `skill_package env_set`、`env_unset`、`env_list` 管理；`env_list` 只返回变量名和是否配置，不返回值。

## 6. 私有状态边界

运行状态、缓存、会话、数据库、下载文件和其他设备私有数据放在：

```text
~/.agentdock/skill-data/<skill-name>/
```

这些内容不得被打包：

- 会话文件和认证缓存；
- Cookie 和浏览器 storage state；
- 数据库；
- 截图和运行结果；
- 下载文件；
- 设备标识和用户私有路径快照。

## 7. 包内禁止项

禁止：

- `agentdock.yaml`；
- `.env` 或其他含真实环境值的文件；
- 密码、Token、Cookie、私钥；
- `__pycache__`、`*.pyc`、`node_modules`；
- 编译产物和大体积生成文件；
- 符号链接；
- 固定用户绝对路径；
- 隐蔽下载并执行；
- 未说明的数据上传、删除或权限修改；
- `skill_run`、`skill_env_manage`、`AGENTDOCK_OPERATION`、`PLUGIN_*`；
- 旧式 `operation`、`entrypoint` 清单和旧 Skill Runtime 设计。

## 8. 辅助脚本契约

推荐输入：

```json
{
  "skill_action": "status"
}
```

要求：

- stdin 必须是 JSON 对象；
- 顶层动作字段为 `skill_action`；
- 秘密只从注入环境读取；
- 不把秘密放在命令行参数、输出或日志中；
- 标准输出为结构化 JSON；
- 错误至少包含稳定 `code` 和可读 `message`；
- `status` 默认只读；
- 破坏性动作需要显式确认；
- 网络超时、依赖缺失和平台不支持必须可诊断。

运行时通过 `exec_command` 直接执行当前激活版本脚本，并传入 `skill_env: "<skill-name>"`。

## 9. 验证矩阵

### 文档和目录

- 根目录存在 `SKILL.md`；
- Frontmatter 能被当前解析器读取；
- 正文非空；
- 引用路径真实存在；
- 无空目录、符号链接和路径逃逸。

### 安全

- 搜索密钥、Cookie、Authorization 头和私钥标记；
- 检查所有网络目标；
- 检查 Shell、文件写入、删除、覆盖、上传和权限修改；
- 检查依赖安装、下载后执行和混淆代码；
- 检查包内二进制和生成文件；
- 检查旧式架构术语，只允许在明确的禁止或迁移说明中出现。

### 脚本和测试

- Python 使用编译检查；
- Shell 使用可用的语法检查；
- Node.js 使用项目现有检查方式；
- 运行包内测试；
- 运行只读 `status`；
- 验证错误不会泄露环境值。

### AgentDock 生命周期

- `skill_package validate` 返回 `valid: true`；
- 记录源码摘要；
- `skill_package install` 成功并激活预期版本；
- `agentdock_context` 出现正确名称和描述；
- `read_file skill://<name>/SKILL.md` 返回当前正文；
- 引用可通过 `skill://<name>/references/...` 读取；
- 当前激活包摘要与预期源码一致。

## 10. 作者交付摘要

交付时至少说明：

- Skill 名称和新版本；
- 触发场景和不负责的范围；
- 新增或修改的文件；
- 环境变量名称，不包含值；
- 测试和校验结果；
- 当前激活版本；
- 仍存在的真实限制或风险。
