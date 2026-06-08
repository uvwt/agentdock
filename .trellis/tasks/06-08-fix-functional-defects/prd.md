# 修复 AgentDock 功能缺陷

## Goal

修复上一轮项目审查中确认的高优先级功能缺陷，让浏览器工具、Skill 环境变量管理、Env Verify 和服务元信息在真实使用时行为一致、可诊断、可测试。

## Requirements

- Browser runner 调用必须在普通权限模式下也能收到 `BROWSER_RUNNER_PAYLOAD`、`BROWSER_ARTIFACT_DIR`、`WORKSPACE` 等运行所需环境变量，不依赖 `AGENTDOCK_SKIP_PERMISSION_PROMPTS`。
- Compat env 定义必须只有一个权威来源，并被 Skill Runtime、`env_manage` / `skill_manage` 和 Nexus Agent 复用，避免不同入口看到不同变量列表。
- `env_manage verify` 必须像 `skill_manage run` 一样校验 `input_json`；非法 JSON 直接返回 validation error，不能静默改成 `{}`。同时支持结构化 `input`。
- `server_info.auth_enabled` 必须反映静态 Bearer 或 OAuth 任一认证配置是否启用。

## Acceptance Criteria

- [ ] 新增/更新测试覆盖 browser runner 在 `DangerouslySkipAllPermissions=false` 时仍能读取 payload env。
- [ ] 新增/更新测试证明 compat env 定义在 Skill Runtime、tools 层和 Nexus Agent 层一致。
- [ ] 新增/更新测试覆盖 `env_manage verify` 非法 `input_json` 报错，合法 `input` / `input_json` 正确传递。
- [ ] 新增/更新测试覆盖 OAuth-only 或 Bearer-only 配置下 `server_info.auth_enabled=true`。
- [ ] `go test ./...`、`go vet ./...`、`go build ./cmd/agentdock` 通过。

## Technical Approach

- 抽取共享 compat env 定义，保留现有 compat 行为但消除多份硬编码。
- 给 browser runner 环境构造单独函数，不复用只在 skip-permission 下合并 extra env 的命令环境逻辑。
- 复用 Skill run 的输入解析语义到 Env verify，减少分叉。
- 只改功能行为和测试，不扩大到 OAuth 安全收紧、artifact 鉴权、JSON Schema 扩展、search 流式优化。

## Out of Scope

- OAuth 授权严格化和 artifact 截图鉴权。
- Skill JSON Schema 完整实现或第三方 validator 引入。
- `search_text` 流式 rg 优化。
- 清理本地二进制备份和补齐 Trellis backend 规范文档。

## Technical Notes

- 相关文件：`internal/tools/browser.go`、`internal/tools/command.go`、`internal/tools/env_manage.go`、`internal/tools/skill_manage.go`、`internal/skillruntime/execute.go`、`internal/nexusagent/agent.go`、`internal/tools/runtime.go`。
- 现有质量门槛：`go test ./...`、`go vet ./...`、`go build ./cmd/agentdock`。
