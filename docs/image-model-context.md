# ChatGPT 图片上下文

启用 MCP Apps 后，直连 AgentDock 的 `view_image` 提供共享图片组件。组件预览标准 MCP 图片结果；用户点击“让 ChatGPT 查看图片”后，使用宿主上传接口和 `widgetState.imageIds` 将真实图片加入后续模型轮次。不支持该接口的宿主只提供预览。

这不是同轮无人值守 Computer Use。标准图片字节、输出 schema 和结构化结果保持不变；不使用 OCR，不新增外部网络域名。上传由用户点击触发，不默认存入文件库。关闭 MCP Apps 或工具返回错误时，不添加图片组件结果绑定。

## 共享实现与合并依赖

HTML、宿主桥接和组件行为测试由 `agentdock-protocol/mcpapps` 统一维护，NexusDock 使用同一实现。依赖的上游改动为 [agentdock-protocol #6](https://github.com/uvwt/agentdock-protocol/pull/6)。

当前是配套草稿：`go.mod` 中的 v0.8.1 尚未包含新接口，因此独立构建暂不成立。合并前必须等待协议改动合并并发布，再更新 `go.mod` / `go.sum` 到真实上游版本、移除本段待办并重新运行完整检查。不提交本地路径 replace 或私人 fork 依赖。

审阅时可将本仓库和协议 PR 分支放入临时 Go workspace，使用 `go work init <协议仓库路径> <本仓库路径>`，再设置 `GOWORK` 为该文件绝对路径运行 `make check` 和 `go test -race ./...`。协议仓库另运行 `node --test mcpapps/image.test.mjs`。

## 验收边界

直接 MCP 集成测试覆盖真实 PNG 的 `view_image` 调用、开关、错误结果和共享 HTML 内容一致性。真实 ChatGPT 的识图验收仍须使用 Chrome：提示词不得提供答案，点击组件后核对只能从图片取得的细节。MCP 集成测试通过不等于已完成直连 ChatGPT 的端到端验收。
