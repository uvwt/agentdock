---
name: wallos
description: Use this skill to query and manage subscriptions in a self-hosted Wallos instance through its official HTTP API. Covers subscriptions, monthly cost, categories, currencies, payment methods, household members, and the current user; excludes administrator, OIDC, notification-secret, Fixer, and generic API management.
version: 0.1.1
---

# Wallos Skill

通过 Wallos 官方 HTTP API 查询和管理个人订阅。适用于“查看订阅”“统计某月支出”“新增或调整订阅”“维护订阅分类”等请求。

上游项目：<https://github.com/ellite/Wallos>

## 能力边界

本 Skill 支持：

- 检查 Wallos 连接与 API Key 是否可用；
- 查询当前用户、订阅列表、单个订阅和指定月份总支出；
- 查询分类、货币、支付方式和家庭成员；
- 新增、编辑、删除订阅；
- 新增、编辑、删除分类。

本 Skill 不负责：

- 管理员设置、用户管理和跨用户查询；
- OIDC、密码登录策略和认证系统配置；
- 通知渠道、Fixer、AI 服务等凭据配置；
- 任意或未审查的 Wallos API 调用；
- 直接读写 Wallos SQLite 数据库。

## 环境变量

| 变量 | 类型 | 必填 | 说明 |
|---|---|---:|---|
| `WALLOS_BASE_URL` | config | 是 | Wallos 实例根地址，例如 `https://wallos.example.com` |
| `WALLOS_API_KEY` | secret | 是 | Wallos 当前用户的 API Key |

缺少任一变量时，`status` 只返回配置状态，不发起网络请求；其他动作返回明确错误。

API Key 只从当前进程环境读取。辅助脚本始终使用 POST 表单传递 API Key，不把它放进 URL、日志或返回结果。

## 安全约束

- `base_url` 只允许 `http` 或 `https`，且不能内嵌用户名、密码、查询参数或片段。
- 所有业务动作都限定为 API Key 所属用户；不支持 Wallos 的管理员跨用户参数。
- `delete_subscription` 和 `delete_category` 必须传 `confirmed: true`。
- 新增或编辑订阅时，日期必须使用 `YYYY-MM-DD`，周期取值为：`1=天`、`2=周`、`3=月`、`4=年`。
- 新增订阅必须提供有效的 `payer_user_id`、`payment_method_id` 和 `category_id`。Wallos 详情弹窗会直接读取这些关联项；空关联会导致前端 PHP Warning。
- `logo_url` 会让 Wallos 服务端下载远程图片；只有用户明确提供该 URL 时才传递。
- 返回内容会再次脱敏，避免上游错误消息意外回显 API Key。

## 辅助脚本执行

Skill 本体是本说明文档。需要调用包内辅助脚本时，在 Skill 包根目录运行：

```bash
printf '%s' '{"skill_action":"status"}' | python3 run.py
```

输入必须是 JSON 对象，动作字段统一为 `skill_action`。

## 动作

| 动作 | 用途 | 关键输入 |
|---|---|---|
| `status` | 检查环境配置；配置完整时验证版本接口 | 可选 `base_url`、`timeout` |
| `current_user` | 查询当前 API Key 对应用户 | 无 |
| `list_subscriptions` | 查询订阅列表 | 可选 `member`、`category`、`payment_method`、`state`、`sort`、`convert_currency` |
| `get_subscription` | 查询单个订阅 | `id`，可选 `convert_currency` |
| `monthly_cost` | 查询指定月份总支出 | `month`、`year` |
| `list_categories` | 查询分类 | 无 |
| `list_currencies` | 查询货币和主货币 | 无 |
| `list_payment_methods` | 查询支付方式 | 无 |
| `list_household` | 查询家庭成员 | 无 |
| `add_subscription` | 新增订阅 | `name`、`price`、`currency_id`、`frequency`、`cycle`、`next_payment`、`payer_user_id`、`payment_method_id`、`category_id`，其余字段可选 |
| `edit_subscription` | 编辑订阅 | `id`，以及至少一个需要修改的字段 |
| `delete_subscription` | 删除订阅 | `id`、`confirmed: true` |
| `add_category` | 新增分类 | `name` |
| `edit_category` | 编辑分类 | `id`、`name` |
| `delete_category` | 删除未被使用的非默认分类 | `id`、`confirmed: true` |

## 示例

查询启用中的订阅并换算为主货币：

```json
{
  "skill_action": "list_subscriptions",
  "state": 0,
  "convert_currency": true
}
```

新增按月订阅：

```json
{
  "skill_action": "add_subscription",
  "name": "Example Service",
  "price": 9.99,
  "currency_id": 1,
  "frequency": 1,
  "cycle": 3,
  "next_payment": "2026-08-01",
  "payer_user_id": 1,
  "payment_method_id": 2,
  "category_id": 7,
  "auto_renew": true,
  "notify": true,
  "notify_days_before": 3
}
```

删除动作必须先由用户明确确认，再执行：

```json
{
  "skill_action": "delete_subscription",
  "id": 42,
  "confirmed": true
}
```
