# Browser Control

AgentDock 的 `browser_*` 工具是通用网页控制能力，前端截图检查只是其中一个使用场景。

## Mac mini / Chrome 推荐方式

Mac mini 裸机运行时，优先让 Playwright 使用系统 Google Chrome，避免依赖 Playwright bundled Chromium 缓存：

```json
{
  "action": "start",
  "browser": "chrome",
  "headless": false,
  "url": "http://localhost:5173"
}
```

`browser=chrome` 使用系统 Google Chrome。这样 AI 可以拿到页面文本、截图、console error 和网络失败信息，比纯桌面坐标点击稳定。

不要默认接管用户正在使用的主 Chrome profile。后续如果需要持久登录态，应使用 AgentDock 专用 profile 目录，避免污染用户日常浏览器数据。

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

## 运行依赖

默认 browser runner 使用 `playwright-core`，不自动下载浏览器二进制。macOS / Mac mini 上推荐使用已安装的 Google Chrome；如果 bundled Chromium 缺失，不要提示用户运行 Playwright 浏览器安装命令，应改用 `browser=chrome` 或让 runner 自动 fallback：

```json
{
  "action": "start",
  "browser": "chrome",
  "headless": false,
  "url": "http://localhost:5173"
}
```

部署 runner 时只需要安装或提供 `playwright-core` 依赖；`browser=chrome` 使用系统 Google Chrome，因此要求系统中已安装 Google Chrome。

## 安全默认值

默认 runner 禁用页面脚本执行动作。AI 做网页操作时应优先使用打开、点击、输入、滚动、等待和截图等可观察动作。

`browser_act` / `browser_snapshot` 把截图发布为轻量 Artifact 引用：始终返回 `screenshot.artifact_id`，存在可访问服务地址时额外返回带过期时间的签名 `screenshot.url`，不直接返回 Base64。模型需要查看截图时，再调用 `view_image(artifact_id=...)`。页面文本、console error、网络失败和页面运行错误会继续作为工具结果返回，方便开发和排障闭环。
