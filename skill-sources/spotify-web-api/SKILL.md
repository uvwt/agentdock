---
name: spotify-web-api
description: Use Spotify Web API with official OAuth PKCE. Use to authorize Spotify safely, search tracks, read current playback, and add tracks to playlists without importing browser cookies.
version: 0.1.6
---

# Spotify Web API

Integration guide for Spotify's official Web API. It uses Authorization Code with PKCE, stores tokens locally under the private Skill data directory, and never returns access or refresh tokens in action output.

Use this skill for Spotify playlist edits, current playback lookup, track search, queueing, adding tracks to either a specified playlist or the playlist currently being played, and reading taste signals such as top tracks, top artists, recently played tracks, saved tracks, and local taste profiles.

## 辅助脚本执行

Skill 本身只提供流程说明。确需调用包内辅助脚本时，使用 `exec_command` 直接运行当前版本脚本，不调用任何 Skill 执行入口。

当前版本目录：

```bash
AGENTDOCK_HOME="${AGENTDOCK_HOME:-$HOME/.agentdock}"
SKILL_DIR="$AGENTDOCK_HOME/skill-store/installed/spotify-web-api/0.1.6"
ENV_FILE="$AGENTDOCK_HOME/skill-data/spotify-web-api/.env"
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
| `status` | Show local OAuth/config state without returning secrets. Optionally validate with Spotify. |
| `auth-url` | Create a Spotify OAuth PKCE authorization URL. Add the returned redirect_uri to the Spotify app first. |
| `finish-auth` | Exchange a Spotify callback URL or authorization code for local tokens. Tokens are stored locally and not returned. |
| `current-playback` | Return the current Spotify playback context and current track. |
| `search-track` | Search Spotify tracks and return simplified results. |
| `list-playlists` | List current user's playlists. |
| `top-tracks` | Return the user's top Spotify tracks for short, medium, or long term taste signals. |
| `top-artists` | Return the user's top Spotify artists and genre tags for short, medium, or long term taste signals. |
| `recently-played` | Return recently played tracks, including played_at and playback context. |
| `saved-tracks` | Return tracks saved in the user's Spotify library. |
| `taste-profile` | Build a local taste profile from top tracks, top artists, recent plays, and saved tracks. |
| `add-track-to-playlist` | Add one or more track URIs to a specific playlist ID, URI, URL, or owned playlist name. |
| `add-track-to-current-playlist` | Add track URIs to the playlist that is currently the Spotify playback context. |
| `search-and-add-to-current-playlist` | Search for a track, choose a result by index, and add it to the currently playing playlist context. |
| `add-to-queue` | Add a track URI to the Spotify playback queue. Spotify Premium is usually required. |
