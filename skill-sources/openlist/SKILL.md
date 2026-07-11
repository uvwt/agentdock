---
name: openlist
description: OpenList v4 HTTP API integration for AgentDock: authentication, file browsing/search, safe text uploads, file operations, storage/driver inspection, and restricted generic API calls.
version: 0.2.2
---

# OpenList Skill

把 [OpenList](https://github.com/OpenListTeam/OpenList) v4 的 HTTP API 封装为 AgentDock 原生 Skill。

## 能力

- 服务状态与公开配置检查
- 登录、登出、本地会话保存与清理
- 当前用户查询
- 文件/目录列表、详情、目录树与搜索
- 新建目录、上传 UTF-8 文本文件、重命名、移动、复制、删除
- 管理员存储列表、驱动列表
- 受限通用 `/api/*` JSON 请求

## 默认连接

默认连接 `http://127.0.0.1:5244`。可在调用中传 `base_url`，或设置 `OPENLIST_URL`。

登录成功后默认把 token 保存到 `~/.agentdock/skill-data/openlist/session.json`，目录权限为 `0700`、文件权限为 `0600`。也可在单次调用中传 `token`，或设置 `OPENLIST_TOKEN`。

## 上传

`upload` 操作通过 OpenList 官方 `/api/fs/put` 接口上传 UTF-8 文本，默认限制 1 MiB，最高允许 10 MiB，并校验 SHA-256。

## 安全约束

- `base_url` 仅允许 `http`/`https`，禁止 URL 内嵌用户名和密码。
- 通用请求仅允许 `/api/*` 路径和 GET/POST/PUT/PATCH/DELETE 方法。
- 登录结果默认不回显 token；只有显式传 `return_token: true` 才返回。
- 不包含 OpenList 上游源码或二进制，仅提供 API 适配层；OpenList 本身继续遵循其 AGPL-3.0 许可证。

## 辅助脚本执行

Skill 本身只提供流程说明。确需调用包内辅助脚本时，使用 `exec_command` 直接运行当前版本脚本，不调用任何 Skill 执行入口。

当前版本目录：

```bash
AGENTDOCK_HOME="${AGENTDOCK_HOME:-$HOME/.agentdock}"
SKILL_DIR="$AGENTDOCK_HOME/skill-store/installed/openlist/0.2.2"
ENV_FILE="$AGENTDOCK_HOME/skill-data/openlist/.env"
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
| `status` | Check OpenList public settings endpoint and report connectivity. |
| `login` | Login to OpenList; saves token locally by default and does not echo it unless return_token=true. |
| `logout` | Logout from OpenList and clear the locally saved session by default. |
| `me` | Return the current authenticated OpenList user. |
| `list` | List a directory through /api/fs/list; guest access is supported when OpenList allows it. |
| `get` | Get file or directory details and raw URL through /api/fs/get. |
| `dirs` | List child directories through /api/fs/dirs. |
| `search` | Search the OpenList index. |
| `mkdir` | Create a directory. |
| `rename` | Rename a file or directory. |
| `move` | Move one or more names between directories. |
| `copy` | Copy one or more names between directories. |
| `remove` | Remove one or more names from a directory. |
| `upload` | Upload a small UTF-8 text file through /api/fs/put. Designed for safe automation and verification. |
| `storage-list` | List configured storages (admin token required). |
| `driver-list` | List OpenList storage driver definitions (admin token required). |
| `api-request` | Call an OpenList JSON API endpoint not covered by a dedicated action. |
| `session-status` | Show whether a local OpenList session token is saved without revealing it. |
| `session-clear` | Delete the locally saved OpenList session token. |
