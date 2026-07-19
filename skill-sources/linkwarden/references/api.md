# Linkwarden API 参考

本参考用于 Linkwarden Skill 1.1.0。核对日期：2026-07-18。

## 证据来源

- 官方 OpenAPI 文档：<https://docs.linkwarden.app/api/api-introduction>
- 官方仓库：<https://github.com/linkwarden/linkwarden>
- Context7 文档集：<https://context7.com/linkwarden/linkwarden>
- 当前源码路由：`apps/web/pages/api/v1/`
- 当前字段校验：`packages/lib/schemaValidation.ts`

官方文档确认 API 使用 HTTP Bearer Auth。请求头格式：

```http
Authorization: Bearer <TOKEN>
Accept: application/json
```

## 端点

| 能力 | 方法 | 路径 | 鉴权 |
|---|---|---|---:|
| 实例公开配置 | GET | `/api/v1/config` | 否 |
| 搜索/分页书签 | GET | `/api/v1/search` | 是 |
| 获取书签 | GET | `/api/v1/links/{id}` | 是 |
| 创建书签 | POST | `/api/v1/links` | 是 |
| 更新书签 | PUT | `/api/v1/links/{id}` | 是 |
| 删除书签 | DELETE | `/api/v1/links/{id}` | 是 |
| 重新归档 | PUT | `/api/v1/links/{id}/archive` | 是 |
| 获取高亮 | GET | `/api/v1/links/{id}/highlights` | 是 |
| 集合列表 | GET | `/api/v1/collections` | 是 |
| 获取集合 | GET | `/api/v1/collections/{id}` | 是 |
| 创建集合 | POST | `/api/v1/collections` | 是 |
| 更新集合 | PUT | `/api/v1/collections/{id}` | 是 |
| 删除集合 | DELETE | `/api/v1/collections/{id}` | 是 |
| 标签列表 | GET | `/api/v1/tags` | 是 |
| 获取标签 | GET | `/api/v1/tags/{id}` | 是 |
| 删除标签 | DELETE | `/api/v1/tags/{id}` | 是 |

`GET /api/v1/links` 已被官方标记为废弃，查询应使用 `/api/v1/search`。Skill 不调用废弃列表接口。

## 搜索参数

`GET /api/v1/search` 支持：

| 参数 | 类型 | 说明 |
|---|---|---|
| `searchQueryString` | string | 全文查询字符串 |
| `sort` | number | Linkwarden 排序枚举值；Skill 默认 `0` |
| `cursor` | number | 游标分页位置 |
| `collectionId` | number | 限定集合 |
| `tagId` | number | 限定标签 |
| `pinnedOnly` | boolean | 仅返回置顶书签 |

示例：

```http
GET /api/v1/search?searchQueryString=agentdock&sort=0&collectionId=42
```

## 创建书签

当前 `PostLinkSchema` 接受：

```json
{
  "url": "https://example.com",
  "name": "Example",
  "description": "Reference",
  "collection": { "id": 42 },
  "tags": [
    { "name": "reference" },
    { "name": "ai" }
  ]
}
```

`collection` 可传 `{ "id": 42 }` 或 `{ "name": "Reading" }`。服务端会读取页面标题、识别内容类型、按用户设置检查重复 URL，并开始保存归档。

Skill 第一版只创建 HTTP(S) URL 书签，不封装 PDF/图片文件上传。

## 更新书签

当前 `UpdateLinkSchema` 不是部分更新，它要求完整对象：

```json
{
  "id": 101,
  "name": "Example",
  "url": "https://example.com",
  "description": "Reference",
  "icon": null,
  "iconWeight": null,
  "color": null,
  "collection": {
    "id": 42,
    "ownerId": 1
  },
  "tags": [{ "name": "reference" }],
  "pinnedBy": []
}
```

因此 Skill 的 `update-link` 必须先执行 `GET /api/v1/links/{id}`，将用户指定字段合并到当前对象后再执行 PUT。移动到新集合时，还要读取目标集合以获得真实 `ownerId`。

改变 URL 会让 Linkwarden 删除旧归档路径并重新生成相关归档字段。

## 重新归档

`PUT /api/v1/links/{id}/archive` 会：

1. 将 screenshot、PDF、readable、monolith、preview、lastPreserved 等字段清空；
2. 删除当前集合下的已有归档文件；
3. 返回“Link is being archived.”并让后台任务重新保存。

该操作不可当成普通只读“刷新”，必须获得明确确认。

## 集合

创建集合字段：

```json
{
  "name": "Development",
  "description": "Engineering references",
  "color": "#8B5CF6",
  "icon": "folder",
  "iconWeight": "regular",
  "parentId": 42
}
```

更新集合同样要求完整对象，至少包含：

```json
{
  "id": 42,
  "name": "Development",
  "description": "Engineering references",
  "color": "#8B5CF6",
  "isPublic": false,
  "icon": "folder",
  "iconWeight": "regular",
  "parentId": null,
  "members": [],
  "propagateToSubcollections": false
}
```

Skill 的 `update-collection` 先读取现状并保留当前成员关系。第一版不支持修改集合成员权限。

## 标签

`GET /api/v1/tags` 支持：

| 参数 | 类型 | 说明 |
|---|---|---|
| `cursor` | number | 游标分页位置 |
| `sort` | number | 排序枚举；Skill 默认 `2`（名称升序） |
| `search` | string | 按标签名称过滤 |

Context7 当前记录的标签排序枚举：

- `0`：日期从新到旧
- `1`：日期从旧到新
- `2`：名称 A-Z
- `3`：名称 Z-A
- `4`：书签数从高到低
- `5`：书签数从低到高

`GET /api/v1/tags/{id}` 返回标签详情，并包含当前关联数量：

```json
{
  "response": {
    "id": 77,
    "name": "legacy-tag",
    "_count": { "links": 0 }
  }
}
```

`delete-tag` 在提交 `DELETE /api/v1/tags/{id}` 前会读取该对象，并要求调用方同时确认当前名称和 `_count.links`。这可以避免标签在确认后新增关联时仍被误删。

## 响应与错误

Linkwarden 多数 v1 端点使用以下响应包裹：

```json
{
  "response": {}
}
```

搜索和标签端点可能直接返回 `data` 与 `message`。Skill 保留服务端原始 JSON，不自行伪造成功结果。

常见状态：

- `200`：请求成功；
- `400`：字段校验失败、只读演示实例或业务限制；
- `401`：Token 无效或没有集合权限；
- `404`：对象不存在；
- `409`：启用防重复时书签已经存在。
