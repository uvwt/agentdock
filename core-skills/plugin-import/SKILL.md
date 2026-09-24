---
name: plugin-import
description: 当用户要安装或更新来自 Git、GitHub、外部插件市场或其他远程来源的 Plugin 时使用；负责把远程来源固定并取得到本地，再交给 plugin_manage 自动识别 Portable/OpenAI/Claude 格式、审核和安装。
---

# Plugin Import

用于把 Git、GitHub、外部 marketplace/catalog 等远程 Plugin 来源安全取得到本地。

`plugin_manage` 直接接受本地目录或 ZIP，并自动识别 AgentDock Portable、OpenAI 和 Claude Plugin 格式；OpenAI/Claude 的兼容性转换由 Core 在审核前完成，不需要模型手工改写 manifest 或重新打包。这个 Skill 只负责远程来源发现/固定/获取，以及 Core 尚不认识的其他格式。最终包的安全校验、规范化摘要、review token、安装事务和运行时激活仍由 `plugin_manage` 负责。

## 什么时候使用

以下情况使用本 Skill：

- 用户给出 Git / GitHub 仓库或仓库子目录；
- 用户给出外部 marketplace/catalog 条目；
- 用户要求更新一个需要重新从远程上游取得内容的 Plugin；
- 来源格式不是 AgentDock Core 已能自动识别的 Portable/OpenAI/Claude。

如果用户已经提供本地目录或 ZIP，不论它是 Portable、OpenAI 还是 Claude Plugin，都直接使用 `plugin_manage`，不要先手工转换。

## 职责边界

本 Skill 负责：

1. 获取外部内容并尽量固定可追踪的 revision/digest；
2. 对明确的远程来源生成根目录 `.agentdock-import.json`，把用户给出的来源和已验证的 revision/ref/subdir 交给 Core；
3. 把目标 Plugin 目录或 ZIP 准备到本地；
4. 对 Core 尚不认识的其他格式，才由模型做显式转换；
5. 调用 `plugin_manage validate`，检查自动识别结果、format、warnings、provenance；
6. 使用 validate 返回的 review token 安装或更新。

本 Skill 不负责绕过 Core 校验，也不要对 OpenAI/Claude 包做重复转换。

## 导入流程

1. **取得来源**
   - 使用宿主已有的命令、文件、浏览器或连接器能力取得内容。
   - Git 来源应尽量固定到具体 commit；下载归档应尽量记录可验证的 digest。
   - 不把凭据、token、带密码的 URL 写进包或 provenance。

2. **准备本地输入**
   - 如果目标已经是一个本地目录或 ZIP，直接保留原样。
   - OpenAI `.codex-plugin/plugin.json`、Claude `.claude-plugin/plugin.json` 和 AgentDock `plugin.json` 都交给 Core 自动识别。
   - 不要为了“兼容”先删除 commands/note 或第三方扩展字段；Core 会保留未知内容，只对自己真正执行的语义做严格判断。

3. **交接远程来源**
   - 只有当来源是用户明确给出的远程地址，或本次获取流程能够可靠证明远程来源时，才在目标 Plugin 根目录写 `.agentdock-import.json`。
   - sidecar 只允许 `origin`、`ref`、`revision`、`subdir`。
   - `origin` 使用稳定、无凭据的上游地址；Git 来源的 `revision` 优先填写实际解析到的 commit SHA，`ref` 只在用户输入或实际仓库状态能确认时填写，`subdir` 只在来源明确指向仓库子目录时填写。
   - GitHub/GitLab 的 tree/blob 子目录 URL 要拆成稳定仓库 `origin` + 已确认的 `ref` + Plugin `subdir`，不要把浏览器页面 URL 整体当作长期 `origin`。
   - 不确定的字段留空，不要根据仓库名、默认分支或 URL 习惯猜测。
   - 用户只给本地目录或本地 ZIP，且无法证明其远程来源时，不创建 sidecar；Core 会使用内容摘要作为匿名本地 provenance。
   - Core 只在自己的 staging snapshot 中读取并移除 sidecar；它不会修改原始来源，也不会把 sidecar 安装进最终 Plugin。

4. **仅在 Core 不认识格式时显式转换**
   - 对其他生态格式，模型才建立 Portable 目录并写 `plugin.json` / `mcp.json`。
   - 对认证、执行权限、hook、agent 等存在语义差异的能力，不要臆造等价行为。
   - 第三方未知元数据无需人工删除或改写；只在需要把完全陌生格式转换成 Portable 时生成 AgentDock 真正使用的运行字段。

5. **验证并安装**
   - 先调用 `plugin_manage(action="validate", source=<本地目录或ZIP>)`。
   - 检查 `valid`、`format`、warnings、executables、Skills、MCP、provenance。
   - warning 中若说明某个 MCP/执行能力“preserved but not activated”，表示原内容已保留，但 AgentDock 不会执行它。
   - 安装使用 validate 返回的原样 `review_token`。
   - validate 后如果修改了任何包内容，必须重新 validate；不要复用旧 token。

## 更新外部来源 Plugin

更新时先检查已安装 Plugin 的 provenance，再从其原始来源取得目标版本到本地。对于 Portable/OpenAI/Claude，直接把新的目录或 ZIP 交给 `plugin_manage validate/update`，不要再手工转换。

`plugin_manage update` 不负责寻找上游版本；它只负责自动识别本地输入、规范化、审核并原子切换最终 canonical package。

正常 revision 前进不需要额外的“换源”参数；如果 origin/subdir 等身份发生变化，应在 review 中明确展示，让用户基于最终 package 内容确认。

## 安全要求

- 最终安装输入只能是本地目录或本地 ZIP；Core 自动识别 Portable/OpenAI/Claude，不接受远程 URL。
- 不把 secret 写进 `plugin.json`、`.agentdock-import.json`、Skill、MCP 配置或 provenance。
- 不保留来源仓库中的符号链接、路径逃逸结构或特殊文件。
- 不静默改变 MCP 认证、安全或网络语义。
- 不执行来源仓库的安装脚本、hook 或未知二进制来完成“导入”。
- 外部内容即使来自官方仓库，也必须经过最终 `plugin_manage validate`。
- `review_token` 只确认它绑定的那份最终 package；内容变化后必须重新审核。

Portable Package 的具体结构与 provenance 字段见 `references/portable-plugin.md`。
