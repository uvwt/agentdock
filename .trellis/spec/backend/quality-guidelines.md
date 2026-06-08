# 质量规范

> AgentDock 后端开发代码质量标准。

## 概览

AgentDock 是安全敏感的工具运行时。改动应小而明确、可测试，并符合现有包边界。产品化工作应提升可重复性和清晰度，不用宽泛抽象掩盖风险。

## 必需质量门禁

完成改动前运行完整门禁：

```bash
make check
```

`make check` 会运行：

- `gofmt -w ./cmd ./internal`
- `go test ./...`
- `go vet ./...`
- `go build -trimpath -o ./bin/agentdock ./cmd/agentdock`

局部迭代时，可以先跑最小相关包测试，最后必须用 `make check` 收尾。

## 禁止模式

- workspace 或 host 文件操作不得绕过 `internal/workspace` 路径解析。
- 新增面向用户的工具时，必须同步 registry、schema、dispatch 和测试。
- `read-only` profile 不得暴露写入、命令、桌面动作、Git mutation、Skill mutation 或 Memory mutation 工具。
- 不为具体应用自动化新增旧动态 plugin 路径；使用原生 Skill Runtime 包。
- 不打印或持久化 secret、token、cookie、OAuth code、authorization header 或含 secret 的原始载荷。
- 当结构化 Go API 或现有 helper 能解决问题时，不使用宽泛 shell 执行。
- 不让本地二进制、覆盖率文件、回滚副本或临时调试产物进入 git。

## 必需模式

- 工具返回值必须结构化且有大小边界。
- 命令输出和外部响应使用已有截断与脱敏 helper。
- 修改 path policy、profile、tool descriptor、schema、Skill Runtime 校验、env registry 行为、命令执行或桌面/浏览器自动化时，新增或更新测试。
- 除非收益明确且已记录，否则优先使用标准库，不新增依赖。
- README 保持简洁；详细 runbook 放在 `docs/`。
- macOS host-mode 部署和 Docker 部署分开维护，因为桌面自动化必须运行在宿主机。

## 测试要求

- 工具/profile 改动：更新 `internal/mcp` 或 `internal/tools` 测试，覆盖 profile 暴露和 schema 不变量。
- 路径处理改动：覆盖 workspace 相对路径、绝对路径、父目录缺失和逃逸尝试。
- Skill Runtime 改动：覆盖 manifest 校验、install/run 路径、权限检查和 secret 处理。
- HTTP/auth 改动：覆盖未鉴权和已鉴权行为，并确认不会记录 secret。
- 纯文档改动完成前也至少运行 `make check`，除非当前环境无法执行。

## 代码审查检查清单

- 改动是否保持工具权限边界？
- 改动是否区分 validation、permission、configuration、runtime 和 network failure？
- 返回错误是否可诊断且不泄露敏感值？
- 本地运行产物是否被忽略，并明确记录为仅本地保留？
- README、docs、Makefile 和 scripts 是否描述同一条验证路径？
- 改动是否足够小，便于 review 和 rollback？
