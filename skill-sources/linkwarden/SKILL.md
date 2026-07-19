---
name: linkwarden
description: Manage a Linkwarden instance through its official HTTP API: check configuration, search and inspect bookmarks, collections, tags and highlights, and perform controlled link, collection or tag changes with explicit confirmation for destructive actions.
version: 1.1.0
---

# Linkwarden Skill

通过 Linkwarden 官方 HTTP API 查询和管理书签、集合、标签与高亮。Skill 使用 Bearer Token，只依赖 Python 标准库，不包含 Linkwarden 源码或服务端组件。

## 适用场景

使用本 Skill 处理：

- 检查 Linkwarden 实例版本、连通性和鉴权配置；
- 搜索书签，按集合、标签、置顶状态筛选；
- 查看书签详情、归档状态和高亮；
- 创建或修改书签；
- 查看、创建或修改集合；
- 查询标签；
- 在明确确认后删除书签、集合或标签，以及重新生成归档。

不要使用本 Skill 处理用户注册、密码重置、管理员用户管理、文件上传、Token 创建/吊销、批量删除或任意 API 透传。这些能力不属于当前安全边界。

## 环境变量

| 变量 | 类型 | 必填 | 说明 |
|---|---|---:|---|
| `LINKWARDEN_URL` | config | 是 | Linkwarden 实例根地址，例如 `https://links.example.com` |
| `LINKWARDEN_TOKEN` | secret | 是* | API Bearer Token；除公开状态检查外均必填 |
| `LINKWARDEN_INSECURE_TLS` | config | 否 | 仅自签名证书调试时设为 `1`；会跳过 TLS 证书校验 |

环境变量缺失时，脚本返回结构化错误，不会尝试读取浏览器 Cookie、配置文件或宿主私有目录。

## 安全约束

- 服务地址只允许 `http` 或 `https`，禁止在 URL 中嵌入用户名、密码、查询参数或片段。
- Token 只从当前进程环境读取，不通过参数传递，也不会出现在输出和错误信息中。
- 查询统一使用 `/api/v1/search`；不使用已废弃的 `GET /api/v1/links`。
- 修改书签或集合前先读取当前对象，再构造 Linkwarden 要求的完整请求体，避免未提供字段被意外清空。
- `delete-link`、`delete-collection`、`delete-tag` 和 `rearchive-link` 必须收到 `confirm: true`。
- 删除集合还必须提供与当前集合名称完全一致的 `confirm_name`。
- 删除标签还必须提供与当前标签名称完全一致的 `confirm_name`，以及与当前关联书签数量完全一致的 `confirm_link_count`。
- `rearchive-link` 会删除现有归档文件并触发重新归档，属于有数据影响的操作。
- 不开放批量操作和任意路径请求，避免扩大误操作范围。

## 执行方式

在 Skill 包根目录运行：

```bash
printf '%s' '{"skill_action":"status"}' | python3 run.py
```

输入必须是 JSON 对象，动作字段为 `skill_action`。输出始终为 JSON；HTTP 响应保持在 `response` 字段中。

## 动作

| 动作 | 类型 | 主要输入 | 说明 |
|---|---|---|---|
| `status` | 只读 | 无 | 检查环境配置；已配置 URL 时调用公开 `/api/v1/config` |
| `search` | 只读 | `query?`, `sort?`, `cursor?`, `collection_id?`, `tag_id?`, `pinned_only?` | 搜索或分页读取书签 |
| `get-link` | 只读 | `id` | 获取单条书签完整信息 |
| `get-highlights` | 只读 | `id` | 获取书签高亮 |
| `create-link` | 写入 | `url`, `name?`, `description?`, `collection_id?` 或 `collection_name?`, `tags?` | 创建 URL 书签 |
| `update-link` | 写入 | `id` 以及需要修改的字段 | 读取现状后合并更新；支持名称、URL、描述、样式、集合、标签和置顶用户 ID |
| `rearchive-link` | 破坏性写入 | `id`, `confirm: true` | 清除当前归档并触发重新归档 |
| `delete-link` | 删除 | `id`, `confirm: true` | 删除单条书签 |
| `list-collections` | 只读 | 无 | 获取当前用户可访问的集合 |
| `get-collection` | 只读 | `id` | 获取集合详情 |
| `create-collection` | 写入 | `name`, `description?`, `color?`, `icon?`, `icon_weight?`, `parent_id?` | 创建集合或子集合 |
| `update-collection` | 写入 | `id` 以及需要修改的字段 | 读取现状并保留成员关系后更新 |
| `delete-collection` | 删除 | `id`, `confirm: true`, `confirm_name` | 名称匹配后删除集合 |
| `list-tags` | 只读 | `search?`, `sort?`, `cursor?` | 查询标签，默认按名称升序 |
| `delete-tag` | 删除 | `id`, `confirm: true`, `confirm_name`, `confirm_link_count` | 名称与当前关联数量均匹配后删除单个标签 |

`tags` 推荐传字符串数组：

```json
{
  "skill_action": "create-link",
  "url": "https://example.com",
  "collection_id": 42,
  "tags": ["reference", "ai"]
}
```

修改书签时只提供需要变化的字段：

```json
{
  "skill_action": "update-link",
  "id": 101,
  "name": "新的标题",
  "tags": ["reference", "reviewed"]
}
```

脚本会先读取书签 101，保留未指定字段，再提交 Linkwarden 所需的完整更新对象。

删除标签前必须确认当前名称和影响数量：

```json
{
  "skill_action": "delete-tag",
  "id": 77,
  "confirm": true,
  "confirm_name": "legacy-tag",
  "confirm_link_count": 0
}
```

脚本会先读取标签 77；只要名称或关联数量已经变化，就拒绝删除。

## 操作流程

1. 首次使用先执行 `status`，确认 URL、Token 和实例连通性。
2. 涉及集合或标签名称时，先用 `list-collections` 或 `list-tags` 解析真实 ID。
3. 更新和删除前读取目标对象并向用户复述关键字段；删除标签时还要复述当前关联书签数量。
4. 写操作完成后再次读取目标对象，验证最终状态。
5. HTTP 失败时返回状态码、端点和服务端响应；不要用猜测替代真实失败证据。

接口和字段依据见 `references/api.md`。

## AgentDock 适配

在 AgentDock 中，把环境值配置到该 Skill 的独立环境，再通过绑定当前激活 Skill 的命令执行能力运行 `python3 run.py`。不要手工拼接已安装版本目录，也不要把真实 Token 写入 Skill 包、源码仓库或 Recall。
