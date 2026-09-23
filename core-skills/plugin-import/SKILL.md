---
name: plugin-import
description: 当用户要安装或更新来自 Git、GitHub、外部插件市场、OpenAI/Claude 等非 AgentDock Portable 格式的 Plugin 时使用；负责获取、理解、转换并生成本地 Portable Plugin，再交给 plugin_manage 审核和安装。
---

# Plugin Import

用于把任意外部 Plugin 来源转换成 AgentDock Core 唯一接受的 Portable Plugin。

AgentDock Plugin Core 不负责 Git、GitHub、远程下载、外部 Catalog 或厂商格式兼容。兼容性转换属于本 Skill；最终包的安全校验、摘要、review token、安装事务和运行时激活仍由 `plugin_manage` 负责。

## 什么时候使用

以下情况使用本 Skill：

- 用户给出 Git / GitHub 仓库或仓库子目录；
- 用户给出 OpenAI、Claude 或其他生态的 Plugin；
- 用户给出外部 marketplace/catalog 条目；
- 用户要求更新一个由外部来源导入的 Plugin；
- 输入内容不是已经可由 `plugin_manage` 直接验证的本地 Portable Plugin 目录或 ZIP。

如果用户已经提供本地 AgentDock Portable Plugin 目录或 ZIP，直接使用 `plugin_manage`，不要额外转换。

## 职责边界

本 Skill 负责：

1. 获取外部内容并固定可追踪的 revision/digest；
2. 阅读原始 manifest、Skills、MCP 和其他组件；
3. 判断哪些能力可以无损映射到 Portable Plugin；
4. 必要时修改目录结构、manifest 和 MCP 配置；
5. 生成干净的本地 Portable Plugin 目录或 ZIP；
6. 在 `plugin.json` 中记录原始 provenance；
7. 调用 `plugin_manage validate`，再使用返回的 review token 安装或更新。

本 Skill不负责绕过 Core 校验。不要为了“装得上”删除或弱化最终 Portable Plugin 的安全规则。

## 导入流程

1. **取得来源**
   - 使用宿主已有的命令、文件、浏览器或连接器能力取得内容。
   - Git 来源应尽量固定到具体 commit；下载归档应尽量记录可验证的 digest。
   - 不把凭据、token、带密码的 URL 写进包或 provenance。

2. **检查原始结构**
   - 找出原始 manifest、Skills、MCP 配置以及 commands/hooks/agents 等附加能力。
   - 不依据厂商名称硬猜字段语义；先读取真实内容。

3. **建立 Portable 目录**
   - 根目录必须有 `plugin.json`。
   - Skills 放在 `skills/<skill-name>/`，每个 Skill 必须是有效 Agent Skill。
   - MCP 配置使用根目录 `mcp.json`。
   - 不复制缓存、`.env`、凭据、数据库、构建产物或无关仓库文件。

4. **做兼容性转换**
   - 能等价表达的 Skills/MCP 可以转换。
   - 对认证、执行权限、hook、agent、command 等存在语义差异的能力，不要臆造等价行为。
   - 无法安全映射的能力应在导入结果里明确说明；必要时停止自动安装，让用户知道缺失的能力。
   - 不把未知字段原样塞进 Portable manifest 企图绕过校验。

5. **写 provenance**
   - provenance 描述原始来源，而不是临时 ZIP 路径。
   - Git 仓库通常记录 origin、commit revision、仓库内 subdir、原始 format，以及 `adapted: true`。
   - provenance 属于最终 package 内容，会被 package digest 和 review token 覆盖。

6. **验证并安装**
   - 先调用 `plugin_manage(action="validate", source=<本地目录或ZIP>)`。
   - 检查 `valid`、warnings、unsupported、executables、Skills、MCP、provenance。
   - 安装使用 validate 返回的原样 `review_token`。
   - validate 后如果修改了任何包内容，必须重新 validate；不要复用旧 token。

## 更新外部来源 Plugin

更新时先检查已安装 Plugin 的 provenance，再从其原始来源取得目标版本并重新生成完整 Portable Package。

不要让 `plugin_manage update` 自己寻找上游版本。它只负责审核并原子切换已经准备好的新 Portable Package。

正常 revision 前进不需要额外的“换源”参数；如果 origin/subdir 等身份发生变化，应在 review 中明确展示，让用户基于最终 package 内容确认。

## 安全要求

- 最终安装输入只能是本地 Portable Plugin 目录或本地 ZIP。
- 不把 secret 写进 `plugin.json`、Skill、MCP 配置或 provenance。
- 不保留来源仓库中的符号链接、路径逃逸结构或特殊文件。
- 不静默改变 MCP 认证、安全或网络语义。
- 不执行来源仓库的安装脚本、hook 或未知二进制来完成“导入”。
- 外部内容即使来自官方仓库，也必须经过最终 `plugin_manage validate`。
- `review_token` 只确认它绑定的那份最终 package；内容变化后必须重新审核。

Portable Package 的具体结构与 provenance 字段见 `references/portable-plugin.md`。
