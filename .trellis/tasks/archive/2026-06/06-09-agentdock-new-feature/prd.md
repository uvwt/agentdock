# brainstorm: AgentDock 新功能

## Goal

围绕 AgentDock 的原生 Skill Runtime 设计一个新的开发体验增强功能，让 Skill 作者在安装和运行前更快发现 manifest、入口文件、依赖命令、env 声明和本机兼容性问题。

## What I already know

* 用户选择的方向是：AgentDock 新功能。
* 用户选择的主题是：Skill 开发体验增强。
* AgentDock 当前能力包括工作区文件、命令、Git/GitHub、原生 Skill、Env Registry、MemoryDock、浏览器自动化、macOS 桌面自动化。
* README 将 `make check` 作为快速验证入口，macOS 裸机更新路径是 `make install-macos`、`make restart-macos`、`make smoke-macos`。
* MCP 工具注册集中在 `internal/mcp/registry.go`，运行实现集中在 `internal/tools` 和对应 runtime 包。
* Skill Runtime 当前支持 `list`、`inspect`、`install`、`run`、`rollback`，并已经接入 manifest env 声明与 Env Registry。
* `skill_manage install` 已经会校验 manifest、entrypoint、平台/架构兼容性、env 声明确认、权限命令是否存在、包 digest 和已安装版本冲突。
* `skill_manage inspect` 主要面向已安装 Skill；当前没有面向未安装本地 Skill source 的专门预检动作。
* `env_manage verify` 可以通过运行某个 operation 验证已安装 Skill 的 env/绑定是否可用，但这发生在安装之后。
* 安全模型强调 profile、认证、命令沙箱、日志脱敏；高风险工具不能暴露在 read-only profile。
* MemoryDock 搜索本轮没有返回与本次“新功能头脑风暴”直接相关的项目记忆。

## Assumptions (temporary)

* 新功能应优先以现有 MCP 工具/Runtime 模块扩展，而不是引入重型外部依赖。
* 新功能如果涉及 Skill、Env、Memory、桌面或命令执行，必须明确权限边界、错误诊断和验证方式。
* MVP 应尽量能通过 Go 单元测试和 `make check` 验证。
* 最小实现应优先复用现有 `LoadManifest`、`ValidateManifest`、`ValidatePackageManifest`、dependency command 检查和 env definition 提取逻辑。

## Open Questions

* Skill 开发体验增强的 MVP 应优先做哪一种形态？

## Requirements (evolving)

* 需求必须有明确用户场景，而不是只做内部重构。
* 需求必须说明与现有工具 profile、安全边界、日志脱敏的关系。
* 需求必须定义可测试的验收标准。
* 面向 Skill 作者提供安装前反馈，避免把“发现 manifest/entrypoint/依赖/env 问题”的唯一入口放在实际 install/run 失败之后。
* 输出必须结构化，适合 Codex/Agent 调用后继续执行下一步修复。
* MVP 采用 `skill_manage action=validate`，输入本地 Skill package/source，执行安装前预检但不安装、不激活、不写 Skill state。
* `validate` 入参沿用 `source`，并支持 `max_bytes`、`digest`、`confirmed_no_env`；不要求 `skill`。
* `validate` 返回应包含：
  * `ok`: 工具调用是否完成。
  * `action`: `validate`。
  * `valid`: package 是否通过预检。
  * `source`: 解析后的 source 信息，避免泄露敏感 URL 参数。
  * `digest`: package digest。
  * `manifest`: 解析出的 manifest（当 manifest 有效时）。
  * `env`: manifest/compat env 声明摘要，区分 `plain` 与 `secret`，不包含任何值。
  * `commands`: 每个声明 command 的存在性检查结果。
  * `issues`: 结构化问题列表，至少包含 `code`、`stage`、`message`。
* 对 manifest、entrypoint、compatibility、dependency command、digest、env confirmation 等失败，优先作为 `issues` 返回并设置 `valid=false`，让 Agent 可以一次性看到问题；source 参数缺失或路径无法解析可以继续作为工具级 validation error。
* `validate` 不执行 Skill entrypoint，不访问 Env Registry 中的真实 secret value，不记录 secret。

## Candidate MVPs

### Option A: `skill_manage` 新增 `validate` / `preflight` (selected)

给 `skill_manage` 增加一个只读式预检动作，输入本地 package/source，输出 manifest、digest、entrypoint、compatibility、commands、env declarations、是否需要 `confirmed_no_env` 等结果。它不安装、不激活、不写 state。

优点：最贴近 Skill 作者痛点；实现可以复用现有安装校验；风险小；测试清晰。

代价：主要覆盖安装前问题，不直接生成模板或修复 env。

### Option B: `skill_manage` 新增 `scaffold`

根据 skill name、operation、env 声明生成 `agentdock.yaml` 和最小 `run.py/run.sh` 模板。

优点：对新作者很友好。

代价：容易牵涉模板风格、语言选择、文件写入权限和后续维护；MVP 边界更容易膨胀。

### Option C: `env_manage` 增强缺失 env 诊断

基于已安装 Skill manifest 汇总缺失 env、kind、最近 verify 状态和建议的 `env_manage set/verify` 下一步。

优点：对运行期排障有用。

代价：只服务已安装 Skill，不能解决开发者安装前失败的问题。

## Acceptance Criteria (evolving)

* [x] 选定一个新功能主题。
* [x] 明确 MVP 范围、非目标和风险点。
* [x] 明确需要修改/新增的模块。
* [x] 明确测试与验证命令。
* [x] `skill_manage` input schema action enum 包含 `validate`。
* [x] `skill_manage validate` 对有效本地 package 返回 `valid=true`，包含 manifest、digest、env 和 commands 摘要。
* [x] 对缺失 entrypoint、manifest invalid、dependency missing、no-env 未确认等情况返回 `valid=false` 和结构化 issues，不写 installed skill state。
* [x] 输出 schema 覆盖 `valid`、`issues`、`digest`、`env`、`commands`。
* [x] 单元测试覆盖成功与失败路径。

## Definition of Done (team quality bar)

* Tests added/updated (unit/integration where appropriate)
* Lint / typecheck / CI green
* Docs/notes updated if behavior changes
* Rollout/rollback considered if risky

## Out of Scope (explicit)

* 不做大规模架构重写。
* 不把具体扩展强绑定进核心代码；具体扩展优先通过原生 Skill Runtime 接入。
* 不记录真实密钥、Token、Cookie、私有凭据或一次性排障日志。
* MVP 不生成 Skill 模板，不自动修改 manifest，不设置 env，不运行 Skill operation。
* MVP 支持本地目录、zip 和 HTTP(S) source；HTTP(S) 复用现有下载/解压逻辑，但测试和文档主线聚焦本地目录/zip。

## Technical Notes

* 入口文档：`README.md`
* 安全文档：`docs/security.md`
* MCP 工具注册：`internal/mcp/registry.go`
* 统一工具分发：`internal/tools/unified.go`
* Skill 管理工具：`internal/tools/skill_manage.go`
* Skill Runtime：`internal/skillruntime/`
* Input schema：`internal/mcp/input_schema.go`
* Output schema：`internal/mcp/output_schema.go`
* 相关规范：`.trellis/spec/backend/skill-runtime-guidelines.md`
* 相关测试：`internal/tools/skill_manage_test.go`、`internal/skillruntime/manifest_test.go`

## Verification Plan

* `go test ./internal/skillruntime ./internal/tools ./internal/mcp`
* `go vet ./...`
* `go build ./cmd/agentdock`
* 收尾运行 `make check`
