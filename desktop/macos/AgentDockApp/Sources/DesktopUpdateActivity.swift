enum DesktopUpdateActivity: Equatable {
    case idle
    case checking
    case applying

    // 只读检查不持有全局应用锁；只有真正安装更新时才冻结服务和配置操作。
    var locksApplication: Bool { self == .applying }

    // 同一时间只允许一个检查请求，安装事务期间也不能重新进入检查流程。
    var canCheckForUpdates: Bool { self == .idle }

    // 安装阶段会替换并重启 App，菜单栏入口必须隐藏以避免暴露中间态。
    var statusItemVisible: Bool { self != .applying }
}
