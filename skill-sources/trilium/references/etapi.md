# Trilium ETAPI Reference

本参考依据 TriliumNext/Trilium 官方仓库中的：

- `apps/server/src/assets/etapi.openapi.yaml`
- `apps/server/src/etapi/notes.ts`
- `apps/server/src/etapi/branches.ts`
- `apps/server/src/etapi/attributes.ts`
- `apps/server/src/etapi/attachments.ts`
- `apps/server/src/etapi/revisions.ts`
- `apps/server/src/etapi/special_notes.ts`

首次实现时核对的上游源码版本为 Trilium `0.103.0`。运行时以目标实例实际返回为准。

## 认证

所有正式动作使用：

```http
Authorization: <ETAPI token>
```

Trilium 也接受 `Bearer <token>` 和 Basic Auth，但本 Skill 固定使用原始 ETAPI Token，避免引入多套认证路径。

## 固定端点

| Skill 动作 | 方法与 ETAPI 路径 |
|---|---|
| `status` | `GET /etapi/app-info` |
| `search-notes` | `GET /etapi/notes` |
| `get-note` | `GET /etapi/notes/{noteId}` |
| `get-note-content` | `GET /etapi/notes/{noteId}/content` |
| `create-note` | `POST /etapi/create-note` |
| `update-note` | `PATCH /etapi/notes/{noteId}` |
| `set-note-content` | `PUT /etapi/notes/{noteId}/content` |
| `delete-note` | `DELETE /etapi/notes/{noteId}` |
| `note-history` | `GET /etapi/notes/history` |
| `list-note-revisions` | `GET /etapi/notes/{noteId}/revisions` |
| `get-revision` | `GET /etapi/revisions/{revisionId}` |
| `get-revision-content` | `GET /etapi/revisions/{revisionId}/content` |
| `create-revision` | `POST /etapi/notes/{noteId}/revision` |
| `undelete-note` | `POST /etapi/notes/{noteId}/undelete` |
| `get-branch` | `GET /etapi/branches/{branchId}` |
| `create-branch` | `POST /etapi/branches` |
| `update-branch` | `PATCH /etapi/branches/{branchId}` |
| `delete-branch` | `DELETE /etapi/branches/{branchId}` |
| `refresh-note-ordering` | `POST /etapi/refresh-note-ordering/{parentNoteId}` |
| `get-attribute` | `GET /etapi/attributes/{attributeId}` |
| `create-attribute` | `POST /etapi/attributes` |
| `update-attribute` | `PATCH /etapi/attributes/{attributeId}` |
| `delete-attribute` | `DELETE /etapi/attributes/{attributeId}` |
| `list-note-attachments` | `GET /etapi/notes/{noteId}/attachments` |
| `get-attachment` | `GET /etapi/attachments/{attachmentId}` |
| `get-attachment-content` | `GET /etapi/attachments/{attachmentId}/content` |
| `get-inbox-note` | `GET /etapi/inbox/{date}` |
| `get-day-note` | `GET /etapi/calendar/days/{date}` |
| `get-week-note` | `GET /etapi/calendar/weeks/{week}` |
| `get-month-note` | `GET /etapi/calendar/months/{month}` |
| `get-year-note` | `GET /etapi/calendar/years/{year}` |
| `create-backup` | `PUT /etapi/backup/{backupName}` |

## 字段映射

Skill 输入统一使用 snake_case，发送给 ETAPI 时转换为官方字段：

| Skill 字段 | ETAPI 字段 |
|---|---|
| `parent_note_id` | `parentNoteId` |
| `note_id` | `noteId` |
| `note_position` | `notePosition` |
| `is_expanded` | `isExpanded` |
| `date_created` | `dateCreated` |
| `utc_date_created` | `utcDateCreated` |
| `attribute_id` | `attributeId` |
| `is_inheritable` | `isInheritable` |
| `fast_search` | `fastSearch` |
| `include_archived_notes` | `includeArchivedNotes` |
| `ancestor_note_id` | `ancestorNoteId` |
| `ancestor_depth` | `ancestorDepth` |
| `order_by` | `orderBy` |
| `order_direction` | `orderDirection` |

## Trilium 特有语义

### 笔记与 Branch

Trilium 的笔记实体和树位置是分开的：

- Note 保存标题、类型、正文和属性；
- Branch 表示 Note 在某个父节点下的位置；
- 同一个 Note 可以拥有多个 Branch，也就是 Trilium 的 cloning；
- 创建一个已存在的 `noteId + parentNoteId` Branch 时，服务端会更新已有 Branch，而不是重复创建。

删除 Branch 不是普通的“移动”操作。需要移动笔记时，应先明确原 Branch、新父节点和是否保留克隆关系，再组合创建与删除动作。

### 富文本正文

ETAPI 的 `GET/PUT /notes/{noteId}/content` 直接处理存储正文。对于 `text` 笔记，正文是 HTML。官方内置 MCP 会做 Markdown 转换，但 ETAPI 不会，因此本 Skill不假装提供 Markdown 写入。

### Protected 内容

ETAPI 会拒绝读取或修改 protected 笔记、附件和版本正文。Skill 保留服务端 HTTP 状态和错误对象，不做会话解锁或其他绕过。

### 删除与恢复

`DELETE /notes/{noteId}` 是 Trilium 的软删除路径。`POST /notes/{noteId}/undelete` 只有在仍存在可恢复父 Branch 时才会成功。Skill 对删除要求当前标题匹配，对恢复要求 ID 二次匹配。

### 特殊日期笔记

Inbox、日记、月记和年记接口可能在目标不存在时创建对应特殊笔记。周记依赖 Trilium 的周记配置，未启用时可能返回 404。

## 第一版有意不开放的接口

- `/etapi/auth/login`、`/etapi/auth/logout`：Token 生命周期不应由普通笔记 Skill 管理；
- `/etapi/notes/{noteId}/export`：返回 ZIP 二进制，需要明确安全的文件落盘契约；
- `/etapi/notes/{noteId}/import`：属于批量数据写入；
- 附件创建、修改和删除：二进制编码、大小和覆盖确认需要单独设计；
- 任意 endpoint：避免把固定安全边界退化成通用 HTTP 客户端。
