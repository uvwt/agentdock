---
name: douban-marks
description: 只读查询豆瓣电影、图书、音乐标记列表和电影条目联想。用于读取用户自己的已看、想看、在看等标记；公开主页可直接读，私密列表需要本机环境或 .env 中的 DOUBAN_COOKIE。禁止用于批量采集、绕登录、写入标记或读取非授权账号。
version: 0.1.8
---

# 豆瓣标记

用于只读查询豆瓣标记数据。

## 能力边界

- 读取操作只读；写入操作必须显式要求并默认 `dry_run=true`。
- 不接收明文 cookie 参数；如需登录态，使用 `DOUBAN_COOKIE` 环境变量或本地 `.env`。
- 默认读取公开主页；如果列表私密，必须是用户自己的登录态。
- 不用于批量采集、绕反爬、抓取非授权账号或高频同步。
- 写入只允许操作用户自己的账号；真实提交必须提供 `DOUBAN_COOKIE`、`dry_run=false` 和 `confirm=true`。

## 常用操作

- `movie-suggest`: 搜索电影候选。
- `movie-watched`: 读取电影已看。
- `movie-wish`: 读取电影想看。
- `movie-doing`: 读取电影在看。
- `marks`: 通用读取，`category` 为 `movie`、`book`、`music`，`status` 为 `collect`、`wish`、`do`。
- `status`: 检查网页接口可达性，以及是否配置了 `DOUBAN_USER_ID`、`DOUBAN_COOKIE`。
- `movie-interest-status`: 用登录态检查电影条目标记弹窗。
- `movie-mark-wish`: 标记想看。
- `movie-mark-doing`: 标记在看。
- `movie-mark-watched`: 标记看过，可附带 1-5 星评分。
- `movie-rate`: 提交看过并评分。

## 配置

可在 AgentDock 环境或本地 `.env` 设置：

```text
DOUBAN_USER_ID=你的豆瓣用户 ID
DOUBAN_COOKIE=bid=...; dbcl2=...; ck=...
```

`DOUBAN_COOKIE` 只用于请求头，不会出现在输出里。

## 辅助脚本执行

Skill 本体是本说明文档。确需调用包内辅助脚本时，在 Skill 包根目录使用相对路径执行；运行宿主负责切换到包根目录并把所需变量注入当前子进程。

```bash
printf '%s' '{"skill_action":"<动作>"}' | python3 run.py
```

输入必须是 JSON 对象。写操作仍按本文档中的确认规则执行。

| 动作 | 用途 |
|---|---|
| `status` | 检查豆瓣网页接口可达性，以及本地是否配置了 DOUBAN_USER_ID 和 DOUBAN_COOKIE。 |
| `movie-suggest` | 查询豆瓣电影联想候选，返回标题、年份、豆瓣 ID、类型、海报和 URL。 |
| `marks` | 读取豆瓣用户标记列表。category 支持 movie/book/music；status 支持 collect/wish/do。默认从 DOUBAN_USER_ID 取用户 ID。 |
| `movie-watched` | 读取电影已看列表，等价于 marks(category=movie,status=collect)。 |
| `movie-wish` | 读取电影想看列表，等价于 marks(category=movie,status=wish)。 |
| `movie-doing` | 读取电影在看列表，等价于 marks(category=movie,status=do)。 |
| `movie-interest-status` | 用登录态读取电影条目标记弹窗，检查是否能提取当前兴趣状态。需要 DOUBAN_COOKIE。 |
| `movie-mark-wish` | 将电影标记为想看。默认 dry_run=true；真实写入必须 dry_run=false 且 confirm=true，并需要 DOUBAN_COOKIE。 |
| `movie-mark-doing` | 将电影标记为在看。默认 dry_run=true；真实写入必须 dry_run=false 且 confirm=true，并需要 DOUBAN_COOKIE。 |
| `movie-mark-watched` | 将电影标记为看过，可附带 1-5 星评分。默认 dry_run=true；真实写入必须 dry_run=false 且 confirm=true，并需要 DOUBAN_COOKIE。 |
| `movie-rate` | 给电影评分；本质上提交看过 collect + rating。默认 dry_run=true；真实写入必须 dry_run=false 且 confirm=true，并需要 DOUBAN_COOKIE。 |
