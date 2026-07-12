---
name: grok-quota
description: Query detailed Grok weekly usage, GrokChat/GrokBuild breakdown, plan, monthly credits, and pay-as-you-go state from local xAI OAuth credentials.
version: 0.3.2
---

# Grok Quota

查询 Grok Build / GrokChat 的详细额度、套餐、周周期和月度积分。实现跟随 CPA 管理面板当前使用的 xAI Billing 协议。

## 动作

- `status`：只读扫描本机 xAI OAuth 凭据，返回脱敏账号引用、token/refresh token 是否存在及过期状态；不发起额度请求。
- `query`：优先调用两条只读 Billing 接口并合并结果；正常情况下不发起模型请求，也不消耗模型 token。只有 Billing 接口不可用时，才回退到最小 Responses 探测。

## 额度语义

CPA 新版管理面板会并行读取：

- `GET https://cli-chat-proxy.grok.com/v1/billing?format=credits`
  - 周期类型、周限额已用比例、周期开始/结束时间。
  - `productUsage` 中的 GrokChat、GrokBuild 等产品已用比例。
- `GET https://cli-chat-proxy.grok.com/v1/billing`
  - 月度积分上限、已用积分、账期开始/结束时间。
  - 按量付费上限和已用金额。

Skill 合并两条响应并返回：

- 套餐：当前已知 `US$150` 对应 SuperGrok，`US$1500` 对应 SuperGrok Heavy。
- 周限额：已用百分比、剩余百分比和重置时间。
- 产品拆分：GrokChat、GrokBuild 等各自已用/剩余百分比。
- 月度积分：已用、上限、剩余的美分和美元值，以及账期重置时间。
- 按量付费：是否启用、上限、已用和剩余金额。

当两条 Billing 接口均无法提供有效数据时，`query` 才向 `/v1/responses` 发起一个最小请求。回退探测只用于判断可用性或解析 429 中的 `subscription:free-usage-exhausted`，可能消耗少量 token；此时会明确返回 `exact_details_available=false`。

## 凭据来源

Skill 不接受 access token、refresh token 或 Cookie 作为输入。它只从外部 JSON auth 文件读取 xAI OAuth 凭据。

支持以下凭据来源：

1. 官方 Grok Build CLI：`$GROK_HOME/auth.json` 或当前系统用户的 `~/.grok/auth.json`。
2. 输入 `auth_file`：一个绝对 JSON 文件路径。
3. 输入 `auth_dir`：一个绝对目录路径。
4. 环境变量 `GROK_QUOTA_AUTH_FILE`。
5. 环境变量 `GROK_QUOTA_AUTH_DIR` 或 `CLIPROXY_AUTH_DIR`。
6. 未配置显式来源时，扫描 `~/.cli-proxy-api/` 与 `~/.config/cli-proxy-api/`。

即使已经配置 CPA auth 目录，Skill 仍会同时检查官方 Grok Build CLI 的 `auth.json`。多个账号时，先运行 `status` 获取脱敏 `account_ref`，再把该值传给 `query`。OAuth access token 过期时，Skill 可使用 auth 文件中的 refresh token 和对应 OIDC client ID 在内存中刷新一次，但不会改写原文件。

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

指定账号与回退探测模型：

```json
{
  "skill_action": "query",
  "account_ref": "脱敏账号引用",
  "model": "grok-4.5",
  "timeout_seconds": 20
}
```

## 安全边界

- 不在输入、输出、日志或 Skill 包中保存/返回 access token、refresh token、ID token、Authorization header、邮箱、subject 或原始文件名。
- 账号只使用稳定的短哈希 `account_ref` 表示。
- 带凭据的额度请求只允许访问固定的 `cli-chat-proxy.grok.com/v1/billing`、`/v1/billing?format=credits` 和回退 `/v1/responses`；OAuth 刷新只允许访问 xAI OIDC discovery 返回的 `*.x.ai` HTTPS endpoint。
- OAuth 刷新只在内存中生效，不修改 CLIProxyAPI 登录态。
- 不读取非 xAI/Grok 类型的 auth 文件。

## 辅助脚本执行

Skill 本体是本说明文档。确需查询时，使用 `exec_command` 直接运行当前安装版本的辅助脚本，并传入 `skill_env: "grok-quota"`：

```bash
AGENTDOCK_HOME="${AGENTDOCK_HOME:-$HOME/.agentdock}"
SKILL_DIR="$AGENTDOCK_HOME/skill-store/installed/grok-quota/0.3.2"
printf '%s' '{"skill_action":"status"}' | python3 "$SKILL_DIR/run.py"
```

| 动作 | 用途 |
|---|---|
| `status` | Discover local xAI OAuth auth files and report only redacted account metadata. |
| `query` | Read detailed Billing quota first; fall back to a minimal Grok probe only when Billing data is unavailable. |
