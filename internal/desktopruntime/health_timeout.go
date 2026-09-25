package desktopruntime

import "time"

// WindowsCoreStartTimeout 是 Windows Core 从启动请求到健康端点可用的统一预算。
// Scheduled Task、DPAPI 恢复和 Runtime/Plugin 初始化都可能发生在冷启动路径中；
// 更新、迁移和安装器必须复用同一预算，避免外层更短的健康检查提前误判并触发回滚。
const WindowsCoreStartTimeout = 60 * time.Second
