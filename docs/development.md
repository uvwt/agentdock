# 开发与质量门禁

本文档定义 AgentDock 默认本地开发和产品化检查方式。

## 质量门禁

完成改动前运行完整门禁：

```bash
make check
```

`make check` 会格式化、测试、vet 并构建项目：

```bash
gofmt -w ./cmd ./internal
go test ./...
go vet ./...
go build -trimpath -o ./bin/agentdock ./cmd/agentdock
```

局部快速迭代时，可以先运行包级测试，最后用 `make check` 收尾。

## 本地产物

macOS 裸机部署时，仓库根目录可能包含本地运行产物：

- `agentdock`
- `agentdock.new`
- `agentdock.prev.*`
- `agentdock.bak*`
- `agentdock.killed*`
- `bin/`
- `coverage.out`

这些文件会被 git 忽略。根目录的 `agentdock` 二进制可能是 Mac mini 上由 launchd 管理的当前宿主机二进制，因此普通仓库清理时不要删除它。

清理历史本地产物时使用：

```bash
make clean-local-artifacts
```

该目标只删除被 `.gitignore` 明确覆盖的历史产物和构建目录，不删除当前运行用的根目录 `agentdock` 二进制。

发布产物应在 git 跟踪的源码之外生成，例如放在 `dist/` 或由发布流水线处理。

## 文档边界

- `README.md` 保持简洁：产品摘要、快速验证、常用部署入口和链接。
- 运维 runbook 放在 `docs/`。
- AI/开发者代码规则放在 `.trellis/spec/`。
- 不在文档中记录真实 token、cookie、OAuth code、私有端点或本地 secret 值。

## 改动要求

- 改动范围限制在被修改的包和行为内。
- 新增抽象前优先复用已有 helper 和包模式。
- 保持高风险工具的权限门禁、路径策略、认证和日志脱敏边界。
- 新增具体应用自动化能力应使用原生 Skill Runtime 包，不使用旧动态 plugin 路径。
- 修改工具描述、schema、path policy、权限、Skill Runtime、env registry、命令执行、HTTP auth 或桌面/浏览器自动化时，更新测试。
