# AgentDock Skill 安全审查规范

本规范供 `skill-installation` 在安装、更新或回滚前后使用。审查结果必须基于真实文件和工具输出，不能只依据发布者说明。

## 1. 审查输出

至少记录：

- 来源和发布者；
- Skill 名称、版本和摘要；
- 审查文件清单；
- 网络目标；
- Shell 和子进程行为；
- 文件读取、写入、删除和覆盖范围；
- 权限、启动项和持久化行为；
- 凭据读取方式；
- 外部依赖和下载行为；
- 二进制或无法审查内容；
- 环境变量名称和缺失状态；
- 风险等级；
- 安装、拒绝或需确认的结论。

不得在报告中记录真实密钥、Cookie、Authorization 值或带认证参数的 URL。

## 2. 风险等级

### low

满足以下特征之一：

- 纯文档 Skill；
- 只读脚本，访问范围清楚；
- 不读取敏感文件；
- 不上传用户数据；
- 不修改权限、启动项或系统配置；
- 依赖固定且可审查。

可在基础校验通过后继续。

### medium

存在以下行为，但范围和目的明确：

- 访问已声明的网络 API；
- 在专属 `skill-data` 目录写入状态；
- 安装普通依赖；
- 对用户明确指定的文件执行可逆修改；
- 读取单一、明确授权的凭据来源。

必须展示行为和缺失配置，再按用户原始安装意图继续。

### high

存在以下行为之一：

- 读取敏感凭据或广泛主目录内容；
- 上传本地数据；
- 删除、覆盖或批量修改用户文件；
- 修改权限、启动项、计划任务或持久化配置；
- 下载并执行外部内容；
- 依赖未固定或安装过程影响系统环境；
- 包含无法充分审查的二进制。

必须获得明确确认，且确认应针对具体风险，不接受笼统“继续”。

### blocked

出现以下任一项立即停止：

- 包内真实密钥、Cookie、私钥或认证缓存；
- 自动读取浏览器全部 Cookie；
- 自动扫描用户主目录敏感文件；
- 隐蔽下载并执行；
- 未说明的数据上传；
- 未经确认删除或覆盖数据；
- 路径穿越、符号链接逃逸或写出包边界；
- 修改系统权限或持久化配置而无清楚说明；
- 会被执行但无法审查来源和行为的二进制；
- `agentdock.yaml` 旧清单；
- 试图恢复旧式统一 Skill 执行架构。

## 3. 文件清单检查

审查时列出普通文件、隐藏文件、符号链接和特殊文件。重点检查：

- `SKILL.md`；
- `references/`；
- `scripts/`、`tests/` 和根目录脚本；
- `requirements*.txt`、`pyproject.toml`、`package.json`、锁文件；
- Shell、PowerShell、Python、JavaScript、Go 和二进制文件；
- 压缩包、安装器和生成文件；
- `.env`、认证缓存、数据库和浏览器状态；
- `__pycache__`、`*.pyc`、`node_modules` 和编译产物。

不跟随符号链接。发现特殊文件、设备文件或命名管道时阻止安装。

## 4. Frontmatter 和文档检查

确认：

- `SKILL.md` 位于包根目录；
- `name` 匹配当前命名规则；
- `description` 能区分触发场景；
- `version` 是语义化版本；
- 正文非空；
- 当前正式契约只依赖 `name`、`description`、`version`；
- 环境变量在正文中声明；
- 网络、写入、删除、权限和上传行为在正文中说明；
- 引用路径存在且不越界。

描述含糊不会自动成为安全阻断项，但会导致模型错误选择，应在安装前要求作者修正或转入 `skill-authoring`。

## 5. 凭据和隐私检查

搜索并人工确认：

- API Key、Token、密码、私钥；
- Authorization 头；
- Cookie、storage state、session 文件；
- 云服务凭据；
- SSH 配置和密钥；
- 浏览器配置目录；
- 系统钥匙串或凭据管理器访问；
- 用户主目录、文档、照片和聊天数据库读取；
- 环境变量值是否进入日志或异常。

只声明变量名不构成泄露。示例值必须明显为占位符，不能具有真实凭据格式和可用性。

## 6. 网络行为检查

列出所有：

- 域名、IP 和端口；
- HTTP 方法；
- 上传内容；
- 下载内容；
- 重定向行为；
- 代理和 TLS 配置；
- 动态拼接 URL；
- WebSocket、回调和长连接。

特别关注：

- 将本地文件、日志、剪贴板或浏览器数据上传；
- 下载后直接执行；
- 绕过 TLS 校验；
- 允许任意用户输入 URL 后访问内网；
- 未限制重定向到非预期主机；
- 在 URL 查询参数中携带秘密。

## 7. Shell 和子进程检查

检查：

- `shell=True`、`eval`、反引号和动态命令拼接；
- 用户输入是否进入命令；
- 是否调用 `sudo`、`chmod`、`chown`、`launchctl`、`systemctl`、计划任务；
- 是否安装系统包或全局依赖；
- 是否运行远程脚本；
- 是否隐藏标准输出和错误；
- 是否忽略失败继续执行；
- 是否通过命令行参数传递秘密。

能使用参数数组时不要拼接 Shell 字符串。确需 Shell 时必须有严格输入约束和错误处理。

## 8. 文件系统检查

列出脚本可能访问的路径，判断：

- 是否限定在用户明确指定目录；
- 是否优先使用 `~/.agentdock/skill-data/<skill-name>/`；
- 是否写入源码目录或已安装包；
- 是否遍历整个主目录；
- 是否跟随符号链接；
- 是否允许 `..` 或绝对路径逃逸；
- 删除和覆盖是否有显式确认；
- 临时文件权限是否安全；
- 日志是否包含秘密。

安装、更新和回滚不得自动删除 `skill-data`。

## 9. 依赖和供应链检查

确认：

- 依赖名称和用途；
- 版本是否固定；
- 是否有锁文件；
- 安装范围是项目内、用户级还是系统级；
- 是否执行安装钩子；
- 是否从非官方源下载；
- 是否拉取分支最新内容而非固定提交；
- 是否下载二进制；
- 摘要或签名是否可验证。

未知或拼写近似的依赖需要重点审查。全局安装、远程安装脚本和未固定提交至少评为 `high`。

## 10. 旧式架构检查

搜索：

- `agentdock.yaml`；
- `skill_run`；
- `skill_env_manage`；
- `AGENTDOCK_OPERATION`；
- `PLUGIN_*`；
- 旧式 `operation`、`entrypoint` 清单；
- 统一 Skill 执行器或旧 Skill Runtime 目录和调用方式。

这些词出现在明确的禁止说明或迁移文档中可以接受；出现在可执行设计、清单或调用示例中应阻止安装。

## 11. 环境配置检查

从正文提取变量名，并通过 `skill_package env_list` 检查配置状态。要求：

- 值只保存在 `~/.agentdock/env/skill/<skill-name>.env`；
- 使用 `env_set`、`env_unset`、`env_list` 管理；
- `env_list` 不返回值；
- 安装包、源码和 `skill-data` 不包含环境文件；
- 更新和回滚不覆盖共享环境；
- 缺失必填变量时明确报告不可用能力。

## 12. 安装后验收

至少验证：

- 安装结果名称、版本、摘要和激活状态；
- 状态中的 `active_version`；
- `agentdock_context` 索引；
- `read_file skill://<name>/SKILL.md`；
- 至少一份包内引用；
- 已安装包摘要与审查摘要一致；
- 必填环境状态完整；
- 有辅助脚本时只读 `status`；
- 无秘密进入日志或结果。

纯文档 Skill 没有脚本时，不要求虚构运行检查。

## 13. 回滚验收

确认：

- 已切换到上一已安装版本；
- `skill://` 返回回滚版本正文；
- 索引描述与回滚版本一致；
- 新版本私有状态未删除；
- 环境配置未覆盖；
- 旧版本所需变量仍完整；
- 状态格式不兼容时已停止自动处理并报告。

## 14. 建议报告格式

```text
Skill: <name>
Version: <version>
Source: <safe source label>
Digest: <sha256>
Risk: low | medium | high | blocked

Reviewed:
- <files and areas>

Network:
- <targets and data>

Filesystem:
- <read/write/delete scope>

Credentials:
- <names and access method, never values>

Dependencies:
- <packages and install behavior>

Missing configuration:
- <variable names only>

Decision:
- install | install after confirmation | blocked

Verification:
- <validate/install/index/read/status results>
```
