---
name: vaultwarden-cli
description: 通过 Bitwarden 官方 bw CLI 安全访问自部署 Vaultwarden。默认只返回脱敏元数据，秘密只进入本机剪贴板。
version: 1.0.4
---

# Vaultwarden CLI

该 Skill 不包含第三方 Vaultwarden SDK，也不把密码、TOTP、主密码或 `BW_SESSION` 返回给模型。

## 安全边界

- 唯一的密码库客户端依赖是 Bitwarden 官方包 `@bitwarden/cli`（命令 `bw`）。
- 主密码和两步验证码只由官方 `bw` CLI 在本机交互式终端读取。
- 临时会话存放在 macOS 钥匙串，服务名为 `agentdock-vaultwarden-cli`。
- `search` 和 `item` 只返回脱敏元数据。
- `copy-secret` 只写入本机剪贴板，默认 45 秒后在内容未变化时自动清除。
- 不提供批量导出、批量显示秘密、任意 `bw` 命令透传或密码库写操作。

## 首次配置或会话失效

在 DockMini 本机终端运行：

```bash
cd "$HOME/AgentDock"
python3 skills/vaultwarden-cli/setup.py
```

按照官方 `bw` CLI 的提示输入 Vaultwarden 地址、账号、主密码和两步验证码。

## 可用操作

- `status`：查看 CLI、服务器、登录和安全会话状态。
- `configure-server`：配置 Vaultwarden 地址；远程地址强制 HTTPS。
- `sync`：同步密码库。
- `search`：搜索条目，只返回脱敏元数据。
- `item`：读取单条脱敏元数据。
- `copy-secret`：复制单个字段到本机剪贴板。
- `lock`：锁定密码库并删除钥匙串会话。

## 调用示例

```json
{"query":"GitHub","limit":10}
```

```json
{"item_id":"条目 ID","field":"password","clear_after_seconds":45}
```

自定义字段：

```json
{"item_id":"条目 ID","field":"custom","field_name":"API Key"}
```

## 辅助脚本执行

Skill 本身只提供流程说明。确需调用包内辅助脚本时，使用 `exec_command` 直接运行当前版本脚本，不调用任何 Skill 执行入口。

当前版本目录：

```bash
AGENTDOCK_HOME="${AGENTDOCK_HOME:-$HOME/.agentdock}"
SKILL_DIR="$AGENTDOCK_HOME/skill-store/installed/vaultwarden-cli/1.0.4"
```

调用 `exec_command` 时传入 `skill_env: "vaultwarden-cli"`，由 AgentDock 自动加载该 Skill 的独立环境；不要在命令中手工 `source` 环境文件。

调用动作（`exec_command` 同时传入 `"skill_env": "vaultwarden-cli"`）：

```bash
printf '%s' '{"skill_action":"<动作>"}' | python3 "$SKILL_DIR/run.py"
```

输入必须是 JSON 对象。写操作仍按本文档中的确认规则执行。

| 动作 | 用途 |
|---|---|
| `status` | 检查官方 bw CLI、服务器配置、登录状态和本机安全会话状态，不返回账号或秘密。 |
| `configure-server` | 把官方 bw CLI 指向指定的 Vaultwarden HTTPS 地址。 |
| `sync` | 使用本机钥匙串中的临时会话同步密码库，不输出秘密。 |
| `search` | 搜索密码库并仅返回条目 ID、名称、类型、收藏状态和域名等脱敏元数据。 |
| `item` | 按条目 ID 读取脱敏元数据；不会返回密码、TOTP、备注或自定义字段值。 |
| `copy-secret` | 把单个密码、用户名、TOTP 或指定自定义字段复制到本机剪贴板；秘密永不进入输出，默认 45 秒后自动清除。 |
| `lock` | 锁定密码库并删除本机钥匙串中的临时会话。 |
