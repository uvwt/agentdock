enum UpdateStatusItemVisibility {
    // 检查更新时保留菜单栏入口；真正替换/重启 App 时隐藏，避免暴露中间态。
    static func shouldShow(isUpdating: Bool, isCheckingForUpdate: Bool) -> Bool {
        !isUpdating || isCheckingForUpdate
    }
}
