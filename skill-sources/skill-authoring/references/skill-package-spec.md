# AgentDock Skill 包规范

本规范供 `skill-authoring` 创建或升级第一方 Skill。当前架构是纯文档 Skill，并明确区分可移植核心与 AgentDock 宿主适配。

## 1. 架构边界

标准链路：

```text
宿主发现并读取 SKILL.md
→ 模型理解流程和约束
→ 宿主选择真实工具
→ 必要时在 Skill 包根目录执行辅助脚本
```

Skill 负责说明“应该怎样做”；工具负责真实执行。包内脚本只是模型可选择调用的资源。

AgentDock 的适配链路是：

```text
agentdock_context
→ read_file skill://<name>/SKILL.md
→ 模型理解流程
→ exec_command skill=<name> / file_edit / 浏览器 / MCP 等真实工具
```

AgentDock 适配不是目标 Skill 的核心运行依赖。

## 2. 可移植核心契约

目标 Skill 默认只依赖：

- `SKILL.md`；
- 包内相对路径；
- 当前进程环境变量；
- 运行宿主提供的命令、文件、浏览器或远端工具能力。

有根目录脚本时，执行示例应使用：

```bash
python3 run.py
```

禁止把以下内容作为核心契约：

- AgentDock 已安装版本绝对路径；
- 固定版本号才能定位脚本；
- `AGENTDOCK_HOME` 或 `AGENTDOCK_SKILL_DIR`；
- `skill_env`、`exec_command`、`skill://`；
- 固定用户绝对路径；
- 主动读取宿主私有环境文件。

这些 AgentDock 术语只能出现在独立、可删除的宿主适配或验证说明中。

## 3. 最小包结构

```text
skill-sources/<skill-name>/
└── SKILL.md
```

按需扩展：

```text
skill-sources/<skill-name>/
├── SKILL.md
├── references/
├── scripts/
├── run.py
└── tests/
```

不要创建空目录。

## 4. SKILL.md Schema

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
- `description` 非空，并支持模型稳定选择；
- `version` 是语义化版本；
- Markdown 正文非空；
- 当前解析器只读取 `name`、`description`、`version`；
- 不把未经支持的字段当作正式契约。

## 5. 版本和不可变性

正文、描述、引用、脚本、测试代表的行为契约、依赖或平台要求发生变化时必须递增版本。

同名同版本安装包内容必须相同。AgentDock 拒绝用不同内容覆盖已安装的同一版本。

## 6. 环境变量契约

需要配置时，目标 `SKILL.md` 声明：

```markdown
## 环境变量

| 变量 | 类型 | 必填 | 说明 |
|---|---|---:|---|
| EXAMPLE_BASE_URL | config | 是 | 服务地址 |
| EXAMPLE_API_KEY | secret | 是 | API Key |
```

Skill 包只包含变量名称、类型、必填性、用途和缺失行为。脚本只从当前进程环境读取变量，不读取宿主环境文件。

AgentDock 将环境值保存在独立私有目录，通过 `skill_package env_*` 管理；执行时由 `exec_command skill=<name>` 只注入本次子进程。该保存方式不属于目标 Skill 的通用契约。

## 7. 私有状态边界

运行状态、缓存、会话、数据库、下载文件和其他设备私有数据不得被打包。

在 AgentDock 中它们位于独立的 Skill 数据目录，但目标 Skill 应通过业务变量、用户输入或宿主提供的工作目录获得必要路径，不硬编码 AgentDock 私有目录。

## 8. 包内禁止项

禁止：

- `agentdock.yaml`；
- `.env` 或其他真实环境值文件；
- 密码、Token、Cookie、私钥；
- `__pycache__`、`*.pyc`、`node_modules`；
- 编译产物和大体积生成文件；
- 符号链接；
- 固定用户绝对路径；
- 隐蔽下载并执行；
- 未说明的数据上传、删除或权限修改；
- `skill_run`、`skill_env_manage`、`AGENTDOCK_OPERATION`、`PLUGIN_*`；
- 旧式 `operation`、`entrypoint` 清单和旧 Skill Runtime。

## 9. 辅助脚本契约

推荐输入：

```json
{
  "skill_action": "status"
}
```

要求：

- stdin 是 JSON 对象；
- 顶层动作字段为 `skill_action`；
- 秘密只从当前进程环境读取；
- 不把秘密放入命令行参数、输出或日志；
- stdout 返回结构化 JSON；
- 错误包含稳定 `code` 和可读 `message`；
- `status` 默认只读；
- 破坏性动作需要显式确认；
- 网络超时、依赖缺失和平台不支持必须可诊断；
- 包内文件通过相对路径或脚本自身目录定位。

## 10. skill-authoring lint

第一方 Skill 在安装前必须运行：

```json
{
  "skill_action": "lint",
  "source": "/path/to/skill-source"
}
```

硬错误包括：

- 硬编码 AgentDock 已安装版本路径；
- 依赖 AgentDock 专属目录变量；
- 主动读取 AgentDock 环境文件；
- 固定用户绝对路径。

警告包括：

- 目标 `SKILL.md` 出现 AgentDock 专属工具或 URI；
- 包内有 `run.py`，但文档缺少相对执行说明；
- 候选文本文件过大或不是 UTF-8。

第一方 Skill 要求 `portable=true`，warning 必须修复或确认只属于独立宿主适配。

`skill_package validate` 仍只负责包级合法性和安装门槛，不承担创作质量 lint。

## 11. 验证矩阵

### 文档和目录

- 根目录存在 `SKILL.md`；
- Frontmatter 可解析；
- 正文非空；
- 引用使用包内相对路径且真实存在；
- 无空目录、符号链接和路径逃逸。

### 可移植性

- `skill-authoring lint` 返回 `portable=true`；
- 脚本从 Skill 根目录以相对命令运行；
- 环境只从当前进程读取；
- 移除宿主适配说明后核心流程仍完整。

### 安全

- 搜索密钥、Cookie、Authorization 头和私钥标记；
- 检查网络目标、Shell、文件写入、删除、上传和权限修改；
- 检查依赖安装、下载后执行和混淆代码；
- 检查包内二进制和生成文件；
- 旧架构术语只允许出现在明确禁止或迁移说明中。

### 脚本和测试

- 运行语言语法检查；
- 运行包内测试；
- 运行只读 `status`；
- 验证错误不会泄露环境值。

### AgentDock 生命周期

- `skill_package validate` 返回 `valid: true`；
- `skill_package install` 成功并激活预期版本；
- `agentdock_context` 出现正确名称和描述；
- `read_file skill://<name>/SKILL.md` 返回当前正文；
- 当前激活包引用可读取；
- `exec_command skill=<name>` 能从激活包根目录运行只读检查。

## 12. 作者交付摘要

交付时至少说明：

- Skill 名称和新版本；
- 触发场景和不负责范围；
- 新增或修改文件；
- 环境变量名称，不包含值；
- lint、测试和包校验结果；
- 当前激活版本；
- 真实限制或风险。
