# 图片模型上下文

启用 MCP Apps 后，直连 AgentDock 的 `view_image` 仍返回标准 MCP image content，并额外绑定共享 `Image` 卡片。卡片默认折叠，只显示图片尺寸、格式和当前状态；用户展开并点击“让模型查看图片”后，组件才把本次工具返回的图片提供给后续模型轮次。

图片传递优先使用标准 MCP Apps 能力：支持时通过 `ui/update-model-context` 写入 image content，并通过 `ui/message` 继续当前请求；宿主未暴露标准图片能力时，才回退到 ChatGPT 的 `uploadFile` / `widgetState.imageIds`。宿主仍可对后续消息要求自己的用户确认。

标准图片字节、输出 schema 和结构化结果保持不变。关闭 MCP Apps、工具调用失败或宿主没有模型图片通道时，不改变 `view_image` 的基础图片返回能力；组件最多只提供预览。

## 共享实现与依赖

HTML、资源契约、宿主能力检测、手动确认和组件行为测试统一由 `agentdock-protocol/mcpapps` 维护，AgentDock 与 NexusDock 共用同一 `ImageUIResourceURI` / `ImageUIContract`。

共享 Image MCP App 已由 [agentdock-protocol #7](https://github.com/uvwt/agentdock-protocol/pull/7) 合入协议主干；`go.mod` 锁定其正式 merge commit 对应版本，独立 checkout 可直接构建和测试。

## 验收边界

集成测试覆盖真实 PNG 的 `view_image` 调用、MCP Apps 开关、错误结果、共享 HTML、用户确认后 handoff 以及标准能力优先 / ChatGPT fallback。真实宿主仍需单独做端到端验收，因为单元测试只能证明组件按协议发出请求，不能代表宿主一定以相同方式处理用户确认和模型上下文。
