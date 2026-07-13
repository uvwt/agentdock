---
name: volcengine-ark-quota
description: Query VolcEngine Ark Coding Plan quota usage from the console endpoint using a logged-in cookie or Playwright storage state.
version: 0.1.7
---

# VolcEngine Ark Quota Skill

用于查询火山引擎 Ark 控制台 Coding Plan 页面里的额度/用量。

目标页面：`https://console.volcengine.com/ark/region:cn-beijing/subscription/coding-plan`

## 能力

- `status`：检查 Coding Plan 页面、`GetCodingPlanUsage` 接口是否可达，并检查是否配置了登录态来源。
- `query`：调用控制台接口查询 Coding Plan 的 `QuotaUsage`，默认同时查询订阅状态 `ListSubscribeTrade`。

前端反查到的核心接口：

- `POST https://console.volcengine.com/api/top/ark/<region>/2024-01-01/GetCodingPlanUsage`
- CSRF 头：`X-Csrf-Token`
- 未登录时提供商错误通常是 `NotLogin`。

## 登录态来源

`query` 至少需要一种登录态：

1. `storage_state_path`：Playwright storage state JSON，推荐。
2. `VOLCENGINE_ARK_STORAGE_STATE`：可在 `~/.agentdock/env/skill/volcengine-ark-quota.env` 或当前命令显式环境中配置，指向 storage state 文件。
3. `VOLCENGINE_ARK_COOKIE`：可在同一私有环境文件或当前命令环境中配置原始 Cookie header；不得写入 Skill 包或日志。
4. `cookie` 输入参数：临时 Cookie header，不推荐长期使用。

可选：`VOLCENGINE_ARK_CSRF_TOKEN` 或 `csrf_token`。通常不需要手动传，Skill 会从 Cookie 里找 CSRF，并在缺失时先探测一次再重试。

## 推荐使用流程

1. 用浏览器工具打开目标页面并登录火山引擎控制台。
2. 导出/保存 Playwright storage state。
3. 调用：

```json
{
  "storage_state_path": "/path/to/storage_state.json",
  "region": "cn-beijing"
}
```

## 输出说明

`quota_periods` 会返回三类用量：

- `session` / 当前会话
- `weekly` / 近 1 周
- `monthly` / 近 1 月

每一项包含 `used_percent`、`remaining_percent`、`reset_at`、`reset_seconds` 等字段。不同账号/套餐返回字段可能有差异，`include_raw=true` 可返回已脱敏原始响应用于排障。

## 安全边界

- Skill 不会把 Cookie、CSRF、API Key 等敏感值输出到 stdout。
- Skill 包内不保存登录态；持久登录态应放在 `skill-data/volcengine-ark-quota/`；环境变量单独放在 `~/.agentdock/env/skill/volcengine-ark-quota.env`。
- 这是控制台内部接口封装，不是火山引擎公开 OpenAPI；如果前端接口改版，Skill 可能需要更新。

## 辅助脚本执行

Skill 本体是本说明文档。确需调用包内辅助脚本时，在 Skill 包根目录使用相对路径执行；运行宿主负责切换到包根目录并把所需变量注入当前子进程。

```bash
printf '%s' '{"skill_action":"<动作>"}' | python3 run.py
```

输入必须是 JSON 对象。写操作仍按本文档中的确认规则执行。

| 动作 | 用途 |
|---|---|
| `status` | Check whether the public Coding Plan page and quota endpoint are reachable, and report available local auth sources without exposing secrets. |
| `query` | Query live Coding Plan quota usage and subscription status. Requires a logged-in console cookie or Playwright storage_state path. |
