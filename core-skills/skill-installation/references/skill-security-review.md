# AgentDock Skill 安全审查规范

用于 `skill-installation` 在安装、更新或恢复一个 Skill 来源快照前做安全审查。

## 1. 审查对象

至少检查：

- `SKILL.md`；
- `references/`；
- `scripts/`、`tests/` 和根目录脚本；
- 依赖清单与锁文件；
- Shell、PowerShell、Python、JavaScript、Go 等源码；
- 二进制、压缩包、生成文件；
- 隐藏文件；
- symlink 和特殊文件。

未知包只做静态检查，不先执行脚本。

## 2. 风险分级

| 等级 | 含义 | 默认处理 |
|---|---|---|
| low | 纯文档或明确低风险只读行为 | 可继续 |
| medium | 有明确网络、写入或普通依赖 | 展示风险后继续 |
| high | 敏感凭据、广泛访问、上传、删除、持久化 | 需明确确认 |
| blocked | 不可接受或无法解释的危险行为 | 停止 |

blocked 包括：

- 真实 secret、Cookie、私钥或认证缓存；
- 隐蔽下载并执行；
- 未说明的数据上传；
- 未确认的删除/覆盖；
- 路径穿越或 symlink 逃逸；
- 未说明的系统权限/持久化修改；
- 无法审查且会执行的二进制；
- `agentdock.yaml` 或旧统一 Skill Runtime。

## 3. 文档身份

确认：

- 根目录存在 `SKILL.md`；
- `name` 符合命名规则并与来源目录身份一致；
- `description` 足以区分触发场景；
- 正文非空；
- `name` 遵守 1–64 字符、仅小写 ASCII 字母/数字/`-`、不首尾 `-`、不含连续 `--`；
- `description` 最长 1024 字符；
- 其他第三方 frontmatter 只作为保留元数据，不因 AgentDock 不使用或字段形状不同而阻塞安装；
- 没有把 version、active_version、activate、rollback 设计成 AgentDock 生命周期。

## 4. 凭据与隐私

检查：

- API Key、Token、密码、私钥；
- Authorization header；
- Cookie、storage state、session；
- 云服务与 SSH 凭据；
- 浏览器 profile；
- 钥匙串/凭据管理器访问；
- 用户主目录、文档、照片、聊天数据库；
- 环境值是否进入日志或异常。

报告只记录变量名和访问方式，不记录值。

## 5. 网络

列出：

- 域名/IP/端口；
- HTTP 方法；
- 上传/下载内容；
- 重定向；
- TLS/代理；
- 动态 URL；
- WebSocket/回调。

重点阻止：

- 本地敏感文件或浏览器数据未说明上传；
- 下载即执行；
- 禁用 TLS 校验；
- 任意 URL 导致 SSRF；
- secret 放 URL query。

## 6. Shell 与依赖

检查：

- `shell=True`、`eval`、动态拼接；
- 用户输入进入命令；
- `sudo`、`chmod`、`chown`、`launchctl`、`systemctl`、计划任务；
- 全局依赖安装；
- 远程脚本；
- secret 放命令行参数；
- 安装钩子；
- 未固定分支/提交；
- 未验证下载摘要。

## 7. 文件系统

确认：

- 访问范围与用户意图一致；
- 包内用相对路径；
- 不写 Skill 源码或 managed 安装目录作为持久状态；
- 不遍历整个主目录；
- 不跟随 symlink；
- 不允许 `..` 逃逸；
- 删除/覆盖有确认；
- 临时文件权限安全；
- 日志无 secret。

持久状态在 AgentDock 中属于宿主管理的数据目录，但目标 Skill 不应硬编码这些路径。standalone managed Skill 使用 `~/.agentdock/data/skills/<name>/`；Plugin-owned Skill 使用独立 `~/.agentdock/data/skills/.plugin/<plugin>/<skill>/` 作为 `SKILL_DATA_DIR`，并可额外获得共享 `PLUGIN_DATA_DIR=~/.agentdock/data/plugins/<plugin>/`。两个运行时变量都属于 AgentDock 保留变量，不能作为用户配置项或由请求覆盖。shared/workspace 候选不应得到这两个变量。

## 8. 环境

使用 `skill_manage env_list` 检查变量名称与配置状态。

要求：

- 值由 `env_set/env_unset` 管理；
- `env_list` 不返回值；
- 包、源码和 data 目录不包含环境文件；
- 安装/更新当前包不覆盖环境；
- 缺失必填变量时明确报告不可用能力。

## 9. 安装验收

安装后至少记录：

- 安全来源标签；
- 可验证 source digest（若有）；
- `content_digest`；
- `changed`；
- context 中的 `source_type`、`skill_ref`、`file`；
- 必填环境配置状态；
- 代表性只读验证。

不要记录 Skill version 或 active version，因为 AgentDock 不建立该状态。

## 10. 同名来源

managed/shared/workspace 同名时分别审查来源。

验收必须使用所选候选自己的 `skill_ref` 和 `file`。不得因裸名称相同把 managed 环境注入 shared/workspace，也不得在后续调用里静默换来源。

## 11. 更新与恢复

更新同名 managed Skill：

- 对新来源重新做完整审查；
- 比较文件、网络、权限、依赖、环境声明和数据格式变化；
- 执行 `skill_manage install`；
- 相同 digest 应 no-op；
- 不同 digest 原子替换当前内容。

恢复旧内容时重新安装可信旧来源快照。AgentDock 不提供 rollback API，也不保留可选 revision history。

## 12. remove 与 purge

`skill_manage remove` 默认只移除当前 managed 包，保留：

- `~/.agentdock/env/skill/<name>.env`
- `~/.agentdock/data/skills/<name>/`

只有用户明确要求时使用 `purge=true` 一并删除。

## 13. 报告格式

```text
Skill: <name>
Source: <safe source label>
Source digest: <sha256 if available>
Content digest: <sha256>
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
- <install/context/read/status results>
```
