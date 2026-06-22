# macOS 裸机 launchd 部署

AgentDock 在 CodingMini 上以 macOS 裸机方式运行，适合桌面自动化、原生 Skill 和本机 Docker 服务编排。

## 固定路径

```text
源码目录：$HOME/agentdock
runtime：$HOME/agentdock-runtime
控制目录：$HOME/agentdock-runtime/AgentDock
workspace：$HOME/agentdock-runtime/workspace
启动脚本：$HOME/agentdock-runtime/start-agentdock.sh
launchd：$HOME/Library/LaunchAgents/com.uvwt.agentdock.plist
本地 healthz：http://127.0.0.1:18766/healthz
本地 MCP：http://127.0.0.1:18766/mcp
```

## 更新流程

```bash
cd ~/agentdock
make check
make install-macos
make restart-macos
make smoke-macos
```

`make install-macos` 会执行 gofmt 检查、测试、vet、构建、可用时 ad-hoc codesign，并把旧二进制备份到：

```text
$HOME/agentdock-runtime/backups/agentdock
```

`make restart-macos` 会重启 `com.uvwt.agentdock` 并验证 healthz。

## 任务向量检索配置

AgentDock 不内置 embedding 模型。Mac mini 可在 `$HOME/agentdock-runtime/agentdock.env` 中复用 RecallDock 的 BGE-M3 embedding endpoint，让 `task_manage.template_match` 在关键词/任务类型/设备匹配之外增加可选向量召回：

```env
AGENTDOCK_TASK_VECTOR_SEARCH=true
AGENTDOCK_TASK_EMBEDDING_ENDPOINT=http://127.0.0.1:18788/v1/embeddings
AGENTDOCK_TASK_EMBEDDING_MODEL=BAAI/bge-m3
AGENTDOCK_TASK_VECTOR_TIMEOUT_MS=10000
AGENTDOCK_TASK_VECTOR_MIN_SCORE=0.55
```

endpoint 未配置或调用失败时会自动降级为原有结构化/关键词匹配，不影响任务工具可用性。

## 注意

不要把 `agentdock.prev*`、`agentdock.bak*`、`agentdock.killed*` 等历史二进制留在源码根目录；源码根目录只保留当前 `agentdock` 二进制，历史备份统一放 runtime backup 目录。
