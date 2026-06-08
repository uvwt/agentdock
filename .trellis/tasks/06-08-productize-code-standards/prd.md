# 产品化代码规范化

## Goal

把 AgentDock 从“功能持续迭代中的本地工具运行层”进一步整理成更适合长期维护、部署和对外协作的产品化代码库。第一阶段聚焦代码规范、仓库卫生、质量门禁、错误与日志约定、发布验证路径，避免一次性大规模重构。

## What I Already Know

- 用户目标是“产品化 代码规范化”，倾向于工程化、稳定、清晰边界，而不是临时堆功能。
- AgentDock 是 Go 单仓库，主要入口为 `cmd/agentdock/main.go`，核心实现集中在 `internal/`。
- 仓库已有基础产品面：`README.md`、`docs/`、`Makefile`、Dockerfile、macOS launchd 安装/重启/smoke 脚本。
- 当前验证入口已有 `make check`，包含 `gofmt`、`go test ./...`、`go vet ./...`、`go build`。
- 记忆中的长期原则要求新增能力走原生 Skill Runtime，不再把旧动态 plugin 作为新增入口；README 只记录原则，不写私有值。
- 当前工作区存在多个本地二进制/备份产物，如 `agentdock`、`agentdock.new`、`agentdock.prev.*`，这类文件不应成为产品化仓库表面的长期噪音。
- `.trellis/spec/backend/index.md` 仍是模板状态，错误处理、日志、质量、目录结构等规范文件需要基于真实代码约定补齐。

## Assumptions

- 本任务不改变 AgentDock 的核心产品方向，也不引入笨重依赖。
- 第一阶段目标是“规范化并建立可持续质量栏”，不是重写模块或调整所有 API。
- 如果发现真实缺陷，可以修复，但要保持改动边界清楚，并补测试或说明验证方式。
- 产品化文档应描述原则、流程和公共配置，不记录本机 token、私有 endpoint、Cookie 或敏感路径内容。

## Requirements

- 明确产品化第一阶段的代码规范目标和非目标。
- 清理仓库中影响产品化观感或容易误提交的本地构建产物、备份产物和忽略规则缺口。
- 补齐或修订 Trellis backend 规范，使后续 AI/开发者能遵循真实项目约定：
  - 目录结构与模块边界。
  - 错误处理与可诊断性。
  - 日志与敏感信息保护。
  - 质量门禁与禁止模式。
- 审查现有 Makefile、README、docs、scripts 是否形成一致的产品化开发/验证入口。
- 优先做小步、可验证、可回滚的规范化改动。
- 修改后运行项目真实验证命令，至少包括 `make check` 或等价拆分命令。

## Acceptance Criteria

- [x] 仓库没有产品化明显噪音：本地构建/备份产物被清理或被明确忽略。
- [x] `.trellis/spec/backend/` 中相关规范从模板状态变成 AgentDock 真实约定。
- [x] README / docs / Makefile 中开发验证入口一致，不互相冲突。
- [x] 如修改 Go 代码，新增或更新必要测试。未修改 Go 代码，未新增测试。
- [x] `make check` 通过，或明确记录无法通过的真实原因和剩余风险。
- [x] 不泄露 secret、token、cookie、私有凭据。

## Out of Scope

- 不做全仓库架构重写。
- 不把所有工具 API 一次性重新设计。
- 不迁移到新框架或引入大型依赖。
- 不改 AgentDock Runtime 的真实部署环境，除非用户明确要求更新部署。
- 不把旧动态 plugin 机制重新产品化为新增能力入口。

## Technical Notes

- 已检查：
  - `README.md`
  - `Makefile`
  - `docs/security.md`
  - `docs/troubleshooting.md`
  - `cmd/agentdock/main.go`
  - `internal/`
  - `.trellis/spec/backend/index.md`
- 初步风险点：
  - 根目录存在本地二进制和历史备份产物，容易污染产品化仓库表面。
  - `.trellis/spec/backend/*.md` 仍是模板，无法有效约束后续开发。
  - `README.md` 已很精简，适合作为产品入口；更细规范应进入 `docs/` 或 `.trellis/spec/`。

## Open Questions

- 已确认：第一阶段优先做仓库卫生、质量门禁和规范文档。
- 本轮暂不优先做 CLI/用户体验一致性、Skill Runtime/工具边界大整理或架构级重构。

## Definition of Done

- [x] 需求边界确认。
- [x] 规范和代码改动完成。
- [x] 自动验证通过：`make check`。
- [x] 如产生稳定的新项目约定，按 Trellis 流程判断是否更新 `.trellis/spec/`。
