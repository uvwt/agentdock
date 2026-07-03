# Browser Control

AgentDock 的 `browser_*` 工具是通用网页控制能力，前端截图检查只是其中一个使用场景。

## Mac mini / Edge 推荐方式

Mac mini 裸机运行时，推荐让 Playwright 启动一个 AgentDock 专用的 Microsoft Edge 实例：

```json
{
  "action": "start",
  "browser": "edge",
  "headless": false,
  "url": "http://localhost:5173"
}
```

`browser=edge` 会映射到 Playwright 的 `msedge` channel。这样 AI 可以拿到页面文本、截图、console error 和网络失败信息，比纯桌面坐标点击稳定。

不要默认接管用户正在使用的主 Edge profile。后续如果需要持久登录态，应使用 AgentDock 专用 profile 目录，避免污染用户日常浏览器数据。

## CDP attach

如果确实需要接管一个已经按调试端口启动的 Edge，可以使用 CDP 后端：

```json
{
  "action": "start",
  "backend": "cdp",
  "cdp_url": "http://127.0.0.1:9222"
}
```

普通方式启动的 Edge 不能被随意接管 DOM 和网络日志。CDP 端口必须只监听 `127.0.0.1`，不要暴露到公网。

## 安全默认值

默认 runner 禁用页面脚本执行动作。AI 做网页操作时应优先使用打开、点击、输入、滚动、等待和截图等可观察动作。

截图 artifact、页面文本、console error、网络失败和页面运行错误会作为工具结果返回，方便开发和排障闭环。
