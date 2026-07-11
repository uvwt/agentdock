# search — 搜索

支持多种搜索类型，通过 `scope` 参数切换 tab，来指定不同的搜索结果 tab 页面。

## 接口

`/store/search`

**请求参数：**

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `keyword` | string | 是 | 搜索关键词 |
| `scope` | int | 否 | 搜索类型。Agent 应按下方“scope 选择指引”显式选择；未传时服务端默认 10（电子书） |
| `maxIdx` | int | 否 | 翻页偏移，默认 0 |
| `count` | int | 否 | 每页数量，不传则服务端默认 15。用户未指定数量时不要传此参数 |

**scope 对应关系：**

| scope | 名称 | 说明 |
|-------|------|------|
| `0` | 全部 | 综合搜索，results 中包含多个分组；适合用户只说“搜一下”且未限定类型 |
| `10` | 电子书 | 只搜电子书（不含网文小说）；适合用户明确“搜书/找书/搜某本书” |
| `16` | 网文小说 | 只搜网文小说 |
| `14` | 微信听书 | 有声书/专辑/播客（三者同义） |
| `6` | 作者 | 搜索作者 |
| `12` | 全文 | 搜索书籍正文内容 |
| `13` | 书单 | 搜索书单 |
| `2` | 公众号 | 搜索公众号 |
| `4` | 文章 | 搜索公众号文章 |

**scope 选择指引（Agent 根据用户意图自动选择）：**
- 用户明确说"搜书""找书""查某本书"或请求获取 bookId → `scope=10`（电子书）
- 用户只说"搜一下 xx"，未说明要搜书/作者/文章/公众号等具体类型 → `scope=0`（全部）
- 用户说"网文""网络小说" → `scope=16`（网文小说）；如果只是普通语义中的"小说"且想找书，仍用 `scope=10`
- 用户说"听书""有声书""播客""专辑" → `scope=14`
- 用户说"搜一下 xx 作者""查作者 xx" → `scope=6`
- 用户说"书里提到了 xx""全文搜索" → `scope=12`
- 用户说"有什么书单""推荐书单" → `scope=13`
- 用户说"搜公众号" → `scope=2`
- 用户说"搜文章" → `scope=4`
- 不要把"没特别指定"同时解释成 `scope=10` 和 `scope=0`；判断标准是：有明确找书意图用 `scope=10`，泛搜索用 `scope=0`。

**回包（V3 格式）：**

| 字段 | 说明 |
|------|------|
| `sid` | 搜索会话 ID |
| `hasMore` | 是否有更多（1=有, 0=无） |
| `results` | 搜索结果分组数组 |
| `results[].title` | 分组标题（如"电子书""作者"） |
| `results[].scope` | 分组类型 |
| `results[].scopeCount` | 该分组总结果数 |
| `results[].currentCount` | 本次返回数量 |
| `results[].books` | 书籍/结果数组 |
| `results[].books[].searchIdx` | 搜索序号（用于翻页） |
| `results[].books[].bookInfo` | 书籍信息对象 |
| `results[].books[].bookInfo.bookId` | 书籍唯一标识 |
| `results[].books[].bookInfo.title` | 书名 |
| `results[].books[].bookInfo.author` | 作者 |
| `results[].books[].bookInfo.cover` | 封面图 URL |
| `results[].books[].bookInfo.intro` | 书籍简介 |
| `results[].books[].bookInfo.publisher` | 出版社 |
| `results[].books[].bookInfo.category` | 分类 |
| `results[].books[].bookInfo.payType` | 付费类型 |
| `results[].books[].bookInfo.price` | 价格（分） |
| `results[].books[].bookInfo.soldout` | 是否下架 |
| `results[].books[].readingCount` | 在读人数 |
| `results[].books[].newRating` | 评分（0-100） |
| `results[].books[].newRatingCount` | 评分人数 |
| `results[].books[].newRatingDetail` | 评分标签（如 `{"title":"神作"}` ） |

> `scope=0`（全部）时 results 会返回多个分组（电子书、作者、书单等），每个分组有自己的 title 和 scope。

## 工作流

1. 根据用户意图选择 `scope`，调 `/store/search`。
2. 从 `results` 取搜索结果。单 tab 模式（scope>0）通常只有一个分组；全部模式（scope=0）有多个分组。
3. 展示结果：书名、作者、评分、在读人数、分类。已下架（soldout=1）需标注。
4. 用户选择某本书后，调 `/book/info` 获取完整信息。
5. 翻页：`hasMore` 为 1 时，用最后一条的 `searchIdx` 作为下一页的 `maxIdx`。

## 输出格式
- 搜索结果用编号列表展示，方便用户通过数字选择
- scope=0 时按分组标题（电子书/作者/书单…）分区展示
- 重点展示：书名、作者、评分、在读人数、分类
