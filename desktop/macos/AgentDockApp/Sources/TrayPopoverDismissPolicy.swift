import Foundation

enum TrayPopoverDismissPolicy {
    static let toggleSuppressionInterval: TimeInterval = 0.25

    /// 判断同一次托盘点击是否属于“刚由外部点击关闭后的同一记点击”，避免 toggle 立刻重新打开。
    static func shouldSuppressToggleAfterExternalDismiss(
        lastDismissedAt: TimeInterval?,
        now: TimeInterval,
        suppressionInterval: TimeInterval = toggleSuppressionInterval
    ) -> Bool {
        guard let lastDismissedAt else { return false }
        let delta = now - lastDismissedAt
        return delta >= 0 && delta < suppressionInterval
    }

    /// 判断某个鼠标事件是否应该收起当前展开的托盘 Popover：
    /// 1. 点击落在 Popover 窗口内部时保留；
    /// 2. 点击落在锚点托盘图标按钮上时交由按钮自己的 action 处理 toggle；
    /// 3. 点击落在其他窗口、桌面空白处、其他菜单栏图标或跨 App 区域时立即收起。
    static func shouldDismissForMouseEvent(
        isInsidePopoverWindow: Bool,
        isInsideAnchorWindow: Bool
    ) -> Bool {
        !isInsidePopoverWindow && !isInsideAnchorWindow
    }

    static func shouldDismissForKeyDown(keyCode: UInt16) -> Bool {
        keyCode == 53
    }
}
