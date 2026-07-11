---
name: douyin-hot
description: 获取抖音实时热榜并返回标题、热度和跳转链接。
version: 1.0.7
---

# 抖音热榜获取技能 | Douyin Hot List Fetcher

获取抖音热榜/热搜榜数据，包含热门视频、挑战赛、音乐等多领域热门内容，并输出标题、热度值与跳转链接。
Fetch Douyin hot list/trending data, including popular videos, challenges, music and more, outputting titles, heat values and links.

---

## 功能特性 | Features

- 🔥 **实时热榜 / Real-time Hot List** - 获取抖音最新热门内容 | Get Douyin's latest trending content
- 📊 **热度值 / Heat Values** - 显示每个话题的热度评分 | Show heat scores for each topic
- 🔗 **跳转链接 / Jump Links** - 提供详情页直达链接 | Provide direct links to detail pages
- 🎯 **自定义数量 / Custom Count** - 可指定获取前 N 条数据 | Specify number of items to fetch
- 📱 **多领域内容 / Multi-domain Content** - 热门视频、挑战赛、音乐等 | Videos, challenges, music and more

---

## 快速开始 | Quick Start

```bash
# 获取抖音热榜前 50 条（默认）/ Get Douyin hot list top 50 (default)
node scripts/douyin.js hot

# 获取前 20 条 / Get top 20
node scripts/douyin.js hot 20

# 获取前 10 条 / Get top 10
node scripts/douyin.js hot 10
```

---

## 输出格式 | Output Format

每条热榜包含 / Each hot list item includes:
- 📌 **排名 / Rank** - 热榜排名 | Hot list ranking
- 🔥 **标题 / Title** - 热门话题/视频标题 | Hot topic/video title
- 📊 **热度值 / Heat** - 热度评分 | Heat score
- 🔗 **链接 / Link** - 详情页跳转链接 | Detail page link

---

## 使用示例 | Usage Example

```bash
# 获取热门前 20 / Get top 20 hot items
node scripts/douyin.js hot 20

# 输出示例 / Output example:
# 1. 🔥 xxx话题 / xxx topic
#    热度 / Heat: 1234567
#    链接 / Link: https://www.douyin.com/...
```

---

## 数据来源 | Data Source

抖音网页端公开接口 | Douyin web public interface

---

## 注意事项 | Notes

- ⚠️ 该接口为网页端公开接口，返回结构可能变动
  This interface is a public web interface; structure may change
- ⚠️ 访问频繁可能触发风控
  Frequent access may trigger rate limiting
- ⚠️ 建议合理使用，避免频繁请求
  Recommend reasonable use, avoid frequent requests

---

## 使用场景 | Use Cases

- 📰 热点追踪 / Hot topic tracking
- 📊 内容趋势分析 / Content trend analysis
- 🎯 营销策划参考 / Marketing planning reference
- 📱 社交媒体运营 / Social media operations

---

## Credits / 致谢

基于 [douyin-hot-trend](https://github.com/franklu0819-lang/douyin-hot-trend) 修改
感谢原作者 @franklu0819-lang

---

## License / 许可证

MIT

## 辅助脚本执行

Skill 本身只提供流程说明。确需调用包内辅助脚本时，使用 `exec_command` 直接运行当前版本脚本，不调用任何 Skill 执行入口。

当前版本目录：

```bash
AGENTDOCK_HOME="${AGENTDOCK_HOME:-$HOME/.agentdock}"
SKILL_DIR="$AGENTDOCK_HOME/skill-store/installed/douyin-hot/1.0.7"
ENV_FILE="$AGENTDOCK_HOME/skill-data/douyin-hot/.env"
```

如存在私有环境文件，先加载：

```bash
set -a
[ ! -f "$ENV_FILE" ] || . "$ENV_FILE"
set +a
```

调用动作：

```bash
printf '%s' '{"skill_action":"<动作>"}' | python3 "$SKILL_DIR/run.py"
```

输入必须是 JSON 对象。写操作仍按本文档中的确认规则执行。

| 动作 | 用途 |
|---|---|
| `status` | 检查运行环境和抖音脚本是否可用。 |
| `hot` | 获取抖音热榜。 |
