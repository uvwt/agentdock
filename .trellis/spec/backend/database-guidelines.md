# 存储规范

> AgentDock 持久化状态约定。

## 概览

AgentDock 当前不使用数据库或 ORM。本地持久化状态以小型 JSON 文件形式保存，位置通常在 AgentDock 控制目录或配置指定的状态目录下。

只有有明确边界的运行时元数据才使用文件状态。能用简单 JSON 文档表达的小状态，不要为它引入数据库依赖。

## 当前状态存储

- `internal/commandqueue/store.go` 保存 Nexus 命令队列状态。
- `internal/commandqueue/outbox.go` 保存上传重试 envelope。
- `internal/envregistry/store.go` 保存脱敏后的 Skill 环境变量注册表元数据和值。
- `internal/nexusclient/state.go` 保存 Nexus 设备状态。
- `internal/skillstate/store.go` 保存 active Skill 版本。

## 写入模式

- 显式创建父目录；已有包级 helper 负责权限时，应继续复用。
- 更新持久 JSON 状态时，先写临时文件、设置权限，再原子替换目标文件。
- I/O 错误必须带操作上下文，例如 `fmt.Errorf("write device state: %w", err)`。
- 如果未来可能迁移，磁盘 JSON 结构应保持稳定并带版本。
- 除非该存储明确用于本地 secret 管理，否则不要保存原始 token、cookie、OAuth code 或 secret 值。

## 迁移

当前没有通用迁移框架。如果 JSON 状态结构变化：

- 增加版本字段或向后兼容的 decode 路径。
- 为旧结构和新结构增加聚焦测试。
- 迁移逻辑放在拥有该文件格式的包内。
- 如果已有用户需要手动处理，在 `docs/` 记录运维影响。

## 常见错误

- 新增状态文件却没有 owner package 或测试。
- 直接写目标文件，没有原子替换。
- 未校验 join 结果，导致 workspace 相对路径逃逸。
- 把本机 secret 或私有 endpoint 写进文档、测试或长期记忆。
