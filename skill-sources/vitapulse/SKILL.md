---
name: vitapulse
description: Read the private VitaPulse HealthKit API gateway with scoped tokens, freshness checks, trends, and asynchronous sync requests.
version: 0.2.4
---

# VitaPulse 元息 Skill

通过 VitaPulse 私有 HealthKit API 网关读取用户已同步的健康数据，并创建异步同步请求。

## 操作

- `status`：验证服务、专用 API Token、Scope 和数据新鲜度。
- `today_summary`：查询某日的健康数据汇总，默认今天。
- `latest_metric`：读取某个 HealthKit 类型的最新样本。
- `trend`：读取某类型最近 1–366 天的日趋势。
- `query_samples`：受控查询标准化样本；可显式请求原始字段或软删除历史。
- `freshness`：查询 iPhone 在线、最近同步和按类型新鲜度。
- `sync_request`：创建异步同步请求；不承诺 iOS 立即运行。
- `sync_request_status`：查询同步请求的 pending/running/completed/failed/expired 状态。
- `clinical_summary`：读取医疗记录；需要 Token 显式拥有 `health:clinical:read`。

## 环境变量

- `VITAPULSE_BASE_URL`（plain）：VitaPulse HTTPS 或本机 API 地址。
- `VITAPULSE_API_TOKEN`（secret）：管理端创建的独立 API Token。

Skill 不使用用户名、密码或启动口令，不直读 SQLite。所有 Scope 校验、限流和访问审计都由 VitaPulse API 执行。Token、FHIR 正文和原始健康数据不得写入 Skill 源包、Git、日志或长期记忆。

默认 Token 应只授予：

- `health:summary:read`
- `health:samples:read`
- `health:sync:trigger`
- `health:sync:status`

高敏和医疗记录权限必须单独显式开启。

## 辅助脚本执行

Skill 本体是本说明文档。确需调用包内辅助脚本时，在 Skill 包根目录使用相对路径执行；运行宿主负责切换到包根目录并把所需变量注入当前子进程。

```bash
printf '%s' '{"skill_action":"<动作>"}' | python3 run.py
```

输入必须是 JSON 对象。写操作仍按本文档中的确认规则执行。

| 动作 | 用途 |
|---|---|
| `status` | Verify server health, gateway capabilities, scoped API token, and data freshness. |
| `today_summary` | Read normalized daily summaries for today or a selected date. |
| `latest_metric` | Read the latest normalized sample for one HealthKit type. |
| `trend` | Read daily trend summaries for one HealthKit type. |
| `query_samples` | Query normalized samples with controlled type, time, raw-field, deletion, and limit filters. |
| `freshness` | Read device, overall sync, and per-type freshness status. |
| `sync_request` | Create an asynchronous request that the iPhone claims on its next launch or background wake. |
| `sync_request_status` | Read one asynchronous sync request status. |
| `clinical_summary` | Read authorized clinical record metadata or explicit content when the token has health:clinical:read. |
