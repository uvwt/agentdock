# 抖音热榜获取技能 | Douyin Hot List Fetcher

> 本技能基于 [douyin-hot-trend](https://github.com/franklu0819-lang/douyin-hot-trend) 进行修改和更新，感谢原作者 [@franklu0819-lang](https://github.com/franklu0819-lang)！
> This skill is modified and updated based on [douyin-hot-trend](https://github.com/franklu0819-lang/douyin-hot-trend). Thanks to the original author [@franklu0819-lang](https://github.com/franklu0819-lang)!

获取抖音热榜/热搜榜数据，包含热门视频、挑战赛、音乐等多领域热门内容，并输出标题、热度值与跳转链接。
Fetch Douyin hot list/trending data, including popular videos, challenges, music and more, outputting titles, heat values and links.

---

## 功能特性 | Features

- 🔥 **实时热榜 / Real-time Hot List** - 获取抖音最新热门内容
- 📊 **热度值 / Heat Values** - 显示每个话题的热度评分
- 🔗 **跳转链接 / Jump Links** - 提供详情页直达链接
- 🎯 **自定义数量 / Custom Count** - 可指定获取前 N 条数据
- 📱 **多领域内容 / Multi-domain Content** - 热门视频、挑战赛、音乐等

---

## 快速开始 | Quick Start

```bash
# 获取抖音热榜前 50 条（默认）
node scripts/douyin.js hot

# 获取前 20 条
node scripts/douyin.js hot 20

# 获取前 10 条
node scripts/douyin.js hot 10
```

---

## 输出格式 | Output Format

每条热榜包含：
- 📌 **排名** - 热榜排名
- 🔥 **标题** - 热门话题/视频标题
- 📊 **热度值** - 热度评分
- 🔗 **链接** - 详情页跳转链接

---

## 使用示例 | Usage Example

```bash
# 获取热门前 20
node scripts/douyin.js hot 20

# 输出示例：
# 1. 🔥 xxx话题
#    热度：1234567
#    链接：https://www.douyin.com/...
```

---

## 数据来源 | Data Source

抖音网页端公开接口

---

## 注意事项 | Notes

- ⚠️ 该接口为网页端公开接口，返回结构可能变动
- ⚠️ 访问频繁可能触发风控
- ⚠️ 建议合理使用，避免频繁请求

---

## 使用场景 | Use Cases

- 📰 热点追踪
- 📊 内容趋势分析
- 🎯 营销策划参考
- 📱 社交媒体运营

---

## License

MIT
