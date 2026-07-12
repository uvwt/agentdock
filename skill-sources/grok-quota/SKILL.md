---
name: grok-quota
description: Query Grok Build/free quota status from local xAI OAuth credentials by following CLIProxyAPI's cli-chat-proxy request flow.
version: 0.1.1
---

# Grok Quota

查询 Grok Build / 免费模型的当前可用状态，并在上游返回 `subscription:free-usage-exhausted` 时解析滚动窗口的 token 实际用量和上限。

该 Skill 复用 CLIProxyAPI 的真实协议：OAuth 账号默认向 `https://cli-chat-proxy.grok.com/v1/responses` 发起最小 Responses 请求，并携带 Grok CLI 身份头。

## 动作

- `status`：只读扫描本机 xAI OAuth 凭据，返回脱敏账号引用、token/refresh token 是否存在及过期状态；不发起额度请求。
- `query`：对一个账号执行最小真实探测。若额度耗尽，解析 `actual/limit`、超额量和使用百分比；若请求成功，只能确认账号当前可用并返回本次探测的 token usage，不能伪造滚动窗口剩余额度。

## 额度语义

CLIProxyAPI 没有调用独立的 Grok 余额接口。它是在正常模型请求收到 HTTP 429 后识别：

- `code=subscription:free-usage-exhausted`
- 错误文本中的 `tokens (actual/limit): <实际>/<上限>`
- `Usage resets over a rolling 24-hour window`

因此：

- 只有上游错误文本包含 `actual/limit` 时，才能返回精确额度数字。
- 请求成功时只能说明“当前可用”，不能从单次响应推导精确剩余额度。
- 这是滚动 24 小时窗口；上游未提供精确重置时间时，不生成虚假的 `reset_at`。
- `query` 会产生一次很小的真实模型请求，可能消耗少量 token。

## 凭据来源

Skill 不接受 access token、refresh token 或 Cookie 作为输入。它只从外部 JSON auth 文件读取 xAI OAuth 凭据。

按优先级支持：

1. 输入 `auth_file`：一个绝对 JSON 文件路径。
2. 输入 `auth_dir`：一个绝对目录路径。
3. 环境变量 `GROK_QUOTA_AUTH_FILE`。
4. 环境变量 `GROK_QUOTA_AUTH_DIR` 或 `CLIPROXY_AUTH_DIR`。
5. 当前系统用户的 `~/.cli-proxy-api/` 与 `~/.config/cli-proxy-api/`。

多个账号时，先运行 `status` 获取脱敏 `account_ref`，再把该值传给 `query`。OAuth access token 过期时，Skill 可使用 auth 文件中的 refresh token 在内存中刷新一次，但不会改写原文件。

推荐通过 Skill 独立环境配置 CPA auth 目录：

```bash
GROK_QUOTA_AUTH_DIR=/path/to/cli-proxy-api/auths
```

## 输入示例

状态检查：

```json
{
  "skill_action": "status"
}
```

查询唯一账号：

```json
{
  "skill_action": "query"
}
```

指定账号与模型：

```json
{
  "skill_action": "query",
  "account_ref": "脱敏账号引用",
  "model": "grok-4.5-build-free",
  "timeout_seconds": 20
}
```

## 安全边界

- 不在输入、输出、日志或 Skill 包中保存/返回 access token、refresh token、ID token、Authorization header、邮箱、subject 或原始文件名。
- 账号只使用稳定的短哈希 `account_ref` 表示。
- 带凭据的请求只允许访问 HTTPS 的 `cli-chat-proxy.grok.com`；OAuth 刷新只允许访问 xAI OIDC discovery 返回的 `*.x.ai` HTTPS endpoint。
- OAuth 刷新只在内存中生效，不修改 CLIProxyAPI 登录态。
- 不读取非 xAI/Grok 类型的 auth 文件。

## 辅助脚本执行

Skill 本体是本说明文档。确需查询时，使用 `exec_command` 直接运行当前安装版本的辅助脚本，并传入 `skill_env: "grok-quota"`：

```bash
AGENTDOCK_HOME="${AGENTDOCK_HOME:-$HOME/.agentdock}"
SKILL_DIR="$AGENTDOCK_HOME/skill-store/installed/grok-quota/0.1.1"
printf '%s' '{"skill_action":"status"}' | python3 "$SKILL_DIR/run.py"
```

| 动作 | 用途 |
|---|---|
| `status` | Discover local xAI OAuth auth files and report only redacted account metadata. |
| `query` | Run one minimal Grok request and report availability or parsed exhausted quota. |
