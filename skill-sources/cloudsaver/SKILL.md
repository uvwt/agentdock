---
name: cloudsaver
description: Use this skill when calling the local CloudSaver API for status checks, resource search, Douban hot lists, share parsing, or transfer actions.
version: 0.3.2
---

# CloudSaver Skill

调用 DockMini 本机 CloudSaver HTTP API，支持状态、登录、资源搜索、豆瓣热门、115/夸克分享解析与转存、设置、赞助信息和 Telegram 图片。

## 安全

- CloudSaver 地址仅允许 localhost/127.0.0.1。
- 登录凭据与 token 不写入 Skill 包。
- 持久化状态位于 `~/.agentdock/skill-data/cloudsaver/`。

## 辅助脚本执行

Skill 本身只提供流程说明。确需调用包内辅助脚本时，使用 `exec_command` 直接运行当前版本脚本，不调用任何 Skill 执行入口。

当前版本目录：

```bash
AGENTDOCK_HOME="${AGENTDOCK_HOME:-$HOME/.agentdock}"
SKILL_DIR="$AGENTDOCK_HOME/skill-store/installed/cloudsaver/0.3.2"
ENV_FILE="$AGENTDOCK_HOME/skill-data/cloudsaver/.env"
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
| `cloud115-folders` | List 115 folders. Requires token and configured 115 cookie in CloudSaver settings. |
| `cloud115-save` | Save 115 shared files. Requires token and configured 115 cookie. Body: shareCode, receiveCode?, folderId, fids, fidTokens?. |
| `cloud115-share-info` | Get 115 share info. Requires token. Params: shareCode, optional receiveCode. |
| `douban-hot` | Get Douban hot list via CloudSaver. Inputs: `type` (default `全部`), `category` (default `热门`), `api` (default `movie`), `limit` (default `50`), optional `start`. |
| `get-setting` | Get CloudSaver user/global settings. Requires token. |
| `login` | Login to CloudSaver and return token in response data. Password is passed only to local CloudSaver API. |
| `quark-folders` | List Quark folders. Requires token and configured Quark cookie in CloudSaver settings. |
| `quark-save` | Save Quark shared files. Requires token and configured Quark cookie. Body: shareCode, receiveCode?, folderId, fids, fidTokens?. |
| `quark-share-info` | Get Quark share info. Requires token. Params: shareCode, optional receiveCode. |
| `register` | Register a CloudSaver user with register code. |
| `save-setting` | Save CloudSaver settings. Requires token. Body should include userSettings and optionally globalSetting. |
| `search` | Search CloudSaver resources by keyword. Supports auto-login on token expiry via request_with_auto_login. |
| `search-with-login` | Login to local CloudSaver, keep the token internal, then search resources by keyword. Final output remains redacted by the helper. |
| `sponsors` | Get CloudSaver sponsors list. |
| `status` | Check CloudSaver container and HTTP endpoint status. |
| `tele-images` | Get Telegram images from CloudSaver. |
