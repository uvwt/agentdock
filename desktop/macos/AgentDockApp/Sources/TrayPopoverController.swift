import AppKit
import SwiftUI

private final class DismissMonitorBox {
    var globalMouseMonitor: Any?
    var localEventMonitor: Any?
    var resignationObserver: NSObjectProtocol?
    var keyResignObserver: NSObjectProtocol?

    func removeAll() {
        if let monitor = globalMouseMonitor {
            NSEvent.removeMonitor(monitor)
            globalMouseMonitor = nil
        }
        if let monitor = localEventMonitor {
            NSEvent.removeMonitor(monitor)
            localEventMonitor = nil
        }
        if let observer = resignationObserver {
            NotificationCenter.default.removeObserver(observer)
            resignationObserver = nil
        }
        if let observer = keyResignObserver {
            NotificationCenter.default.removeObserver(observer)
            keyResignObserver = nil
        }
    }

    deinit {
        removeAll()
    }
}

@MainActor
final class TrayPopoverController: NSObject, NSPopoverDelegate {
    private let model = TrayPopoverModel()
    private let popover = NSPopover()
    private weak var anchorButton: NSStatusBarButton?
    private let dismissMonitors = DismissMonitorBox()
    private var lastExternalDismissAt: TimeInterval?
    private var isClosingProgrammatically = false

    init(
        onOpenAgentDock: @escaping () -> Void,
        onOpenSettings: @escaping () -> Void,
        onRunServiceAction: @escaping (String) -> Void,
        onShowUpdateProgress: @escaping () -> Void
    ) {
        super.init()
        popover.behavior = .transient
        popover.animates = true
        popover.delegate = self
        popover.contentSize = NSSize(width: 350, height: 292)
        popover.contentViewController = NSHostingController(
            rootView: TrayPopoverView(
                model: model,
                onOpenAgentDock: { [weak self] in
                    self?.close()
                    onOpenAgentDock()
                },
                onOpenSettings: { [weak self] in
                    self?.close()
                    onOpenSettings()
                },
                onRunServiceAction: { [weak self] action in
                    self?.close()
                    onRunServiceAction(action)
                },
                onShowUpdateProgress: { [weak self] in
                    self?.close()
                    onShowUpdateProgress()
                }
            )
        )
    }

    func update(status: ServiceStatus, isUpdating: Bool, isCheckingForUpdate: Bool) {
        model.status = status
        model.isUpdating = isUpdating
        model.isCheckingForUpdate = isCheckingForUpdate
    }

    func toggle(relativeTo button: NSStatusBarButton) {
        if popover.isShown {
            close()
            return
        }
        // 按钮使用 .leftMouseUp 触发 toggle；若按下阶段刚被全局/本地监听或 transient 关闭，
        // 放开鼠标时不再重复弹出，消除“先关又立刻弹开”的双重抖动。
        if TrayPopoverDismissPolicy.shouldSuppressToggleAfterExternalDismiss(
            lastDismissedAt: lastExternalDismissAt,
            now: ProcessInfo.processInfo.systemUptime
        ) {
            lastExternalDismissAt = nil
            return
        }
        show(relativeTo: button)
    }

    func show(relativeTo button: NSStatusBarButton) {
        guard !popover.isShown else { return }
        anchorButton = button
        lastExternalDismissAt = nil
        popover.show(relativeTo: button.bounds, of: button, preferredEdge: .minY)
        activatePopoverForFocus()
    }

    func close() {
        guard popover.isShown else {
            teardownDismissMonitors()
            return
        }
        isClosingProgrammatically = true
        popover.performClose(nil)
        isClosingProgrammatically = false
        teardownDismissMonitors()
    }

    func popoverWillShow(_ notification: Notification) {
        installDismissMonitors()
    }

    func popoverDidShow(_ notification: Notification) {
        installPopoverWindowKeyResignObserver()
    }

    func popoverDidClose(_ notification: Notification) {
        if !isClosingProgrammatically {
            lastExternalDismissAt = ProcessInfo.processInfo.systemUptime
        }
        teardownDismissMonitors()
    }

    private var popoverWindow: NSWindow? {
        popover.contentViewController?.view.window
    }

    private func installDismissMonitors() {
        teardownDismissMonitors()

        // .accessory 应用在未处于激活状态时，点击其他 App、桌面空白或其他菜单栏图标不会稳定触发
        // NSPopover.Behavior.transient，必须通过全局鼠标监听兜底立即收起。
        dismissMonitors.globalMouseMonitor = NSEvent.addGlobalMonitorForEvents(
            matching: [.leftMouseDown, .rightMouseDown]
        ) { [weak self] _ in
            let screenLocation = NSEvent.mouseLocation
            Task { @MainActor [weak self] in
                guard let self, self.popover.isShown else { return }
                let isInsidePopover = self.popoverWindow?.frame.contains(screenLocation) ?? false
                let isInsideAnchor = self.isScreenPointInsideAnchorButton(screenLocation)
                if TrayPopoverDismissPolicy.shouldDismissForMouseEvent(
                    isInsidePopoverWindow: isInsidePopover,
                    isInsideAnchorWindow: isInsideAnchor
                ) {
                    self.dismissFromExternalInteraction()
                }
            }
        }

        // 点击本应用其他窗口、按 ESC 键或右键点击非 Popover 区域时，同样即时关闭；
        // 点击托盘图标自身时忽略，避免与 NSStatusBarButton 的 toggle 竞争。
        dismissMonitors.localEventMonitor = NSEvent.addLocalMonitorForEvents(
            matching: [.leftMouseDown, .rightMouseDown, .keyDown]
        ) { [weak self] event in
            guard let self else { return event }
            let shouldConsume = MainActor.assumeIsolated { () -> Bool in
                guard self.popover.isShown else { return false }
                if event.type == .keyDown {
                    if TrayPopoverDismissPolicy.shouldDismissForKeyDown(keyCode: event.keyCode) {
                        self.dismissFromExternalInteraction()
                        return true
                    }
                    return false
                }

                let popoverWindow = self.popoverWindow
                let anchorWindow = self.anchorButton?.window
                let isInsidePopover = event.window != nil && event.window === popoverWindow
                let isInsideAnchor = event.window != nil && event.window === anchorWindow && {
                    guard let button = self.anchorButton else { return false }
                    let pointInButton = button.convert(event.locationInWindow, from: nil)
                    return button.bounds.contains(pointInButton)
                }()
                if TrayPopoverDismissPolicy.shouldDismissForMouseEvent(
                    isInsidePopoverWindow: isInsidePopover,
                    isInsideAnchorWindow: isInsideAnchor
                ) {
                    self.dismissFromExternalInteraction()
                }
                return false
            }
            return shouldConsume ? nil : event
        }

        // 切换应用焦点（如 Cmd+Tab 或点击 Dock 切换前台应用）时也一并收起 Popover。
        dismissMonitors.resignationObserver = NotificationCenter.default.addObserver(
            forName: NSApplication.didResignActiveNotification,
            object: nil,
            queue: .main
        ) { [weak self] _ in
            Task { @MainActor [weak self] in
                self?.dismissFromExternalInteraction()
            }
        }
    }

    private func installPopoverWindowKeyResignObserver() {
        if let observer = dismissMonitors.keyResignObserver {
            NotificationCenter.default.removeObserver(observer)
            dismissMonitors.keyResignObserver = nil
        }
        guard let window = popoverWindow else { return }
        dismissMonitors.keyResignObserver = NotificationCenter.default.addObserver(
            forName: NSWindow.didResignKeyNotification,
            object: window,
            queue: .main
        ) { [weak self] _ in
            Task { @MainActor [weak self] in
                self?.dismissFromExternalInteraction()
            }
        }
    }

    private func dismissFromExternalInteraction() {
        guard popover.isShown else {
            teardownDismissMonitors()
            return
        }
        lastExternalDismissAt = ProcessInfo.processInfo.systemUptime
        isClosingProgrammatically = true
        popover.performClose(nil)
        isClosingProgrammatically = false
        teardownDismissMonitors()
    }

    private func teardownDismissMonitors() {
        dismissMonitors.removeAll()
    }

    private func isScreenPointInsideAnchorButton(_ screenPoint: NSPoint) -> Bool {
        guard let button = anchorButton, let window = button.window else { return false }
        let rectInWindow = button.convert(button.bounds, to: nil)
        let rectOnScreen = window.convertToScreen(rectInWindow)
        return rectOnScreen.contains(screenPoint)
    }

    /// .accessory 应用弹出托盘 Popover 时，仅将 Popover 窗口设为 key window；
    /// 若当前没有可见的常规应用窗口（例如主窗口处于关闭/隐藏态），再轻量激活应用使焦点与
    /// ESC / didResignActive / transient 链路完全生效，同时避免把后台常规窗口抬到前台。
    private func activatePopoverForFocus() {
        guard let popoverWindow else { return }
        let hasVisibleRegularWindow = NSApp.windows.contains { window in
            window !== popoverWindow &&
            window !== anchorButton?.window &&
            window.isVisible &&
            window.level == .normal
        }
        if !hasVisibleRegularWindow {
            NSApp.activate(ignoringOtherApps: true)
        }
        popoverWindow.makeKey()
    }
}

@MainActor
private final class TrayPopoverModel: ObservableObject {
    @Published var status: ServiceStatus = .missing
    @Published var isUpdating = false
    @Published var isCheckingForUpdate = false
}

private struct TrayPopoverView: View {
    @ObservedObject var model: TrayPopoverModel
    let onOpenAgentDock: () -> Void
    let onOpenSettings: () -> Void
    let onRunServiceAction: (String) -> Void
    let onShowUpdateProgress: () -> Void

    private var statusTitle: String {
        if model.isCheckingForUpdate {
            return L10n.text("Checking for updates…")
        }
        if model.isUpdating {
            return L10n.text("Updating…")
        }
        if !model.status.installed { return L10n.text("Not installed") }
        if model.status.healthy { return L10n.text("Running normally") }
        if model.status.requiresApproval { return L10n.text("Background permission required") }
        if model.status.loaded { return L10n.text("Service error") }
        return L10n.text("Stopped")
    }

    private var statusColor: Color {
        if model.isUpdating || model.isCheckingForUpdate { return .accentColor }
        if model.status.healthy { return .green }
        if model.status.loaded || model.status.requiresApproval { return .orange }
        return .secondary
    }

    private var subtitle: String {
        if model.isCheckingForUpdate {
            return L10n.text("Checking for updates…")
        }
        if model.isUpdating {
            return L10n.text("Updating AgentDock…")
        }
        if model.status.healthy {
            return L10n.text("AgentDock is running")
        }
        if model.status.loaded {
            return L10n.text("AgentDock needs attention")
        }
        return model.status.installed
            ? L10n.text("AgentDock is stopped")
            : L10n.text("Set up AgentDock to get started.")
    }

    private var coreText: String {
        if model.status.healthy { return L10n.text("Running") }
        if model.status.loaded { return L10n.text("Unavailable") }
        return L10n.text("Stopped")
    }

    private var nexusText: String {
        switch model.status.nexusConnection {
        case .connected: return L10n.text("Connected")
        case .disconnected: return L10n.text("Not connected")
        case .configurationError: return L10n.text("Unavailable")
        case .unconfigured: return L10n.text("Not configured")
        }
    }

    private var serviceAction: String {
        model.status.loaded ? "restart" : "start"
    }

    private var serviceActionTitle: String {
        model.status.loaded
            ? L10n.text("Restart AgentDock")
            : L10n.text("Start AgentDock")
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack(spacing: 11) {
                AgentDockLogoView(size: 38)
                VStack(alignment: .leading, spacing: 2) {
                    Text("AgentDock")
                        .font(.system(size: 16.5, weight: .semibold))
                    Text(subtitle)
                        .font(.system(size: 11.5))
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
                Spacer(minLength: 8)
                HStack(spacing: 5) {
                    Circle()
                        .fill(statusColor)
                        .frame(width: 6, height: 6)
                    Text(statusTitle)
                        .font(.system(size: 11, weight: .semibold))
                }
                .padding(.horizontal, 8)
                .padding(.vertical, 5)
                .background(statusColor.opacity(0.12), in: Capsule())
            }

            VStack(spacing: 0) {
                statusRow(symbol: "cpu", title: L10n.text("Core service"), value: coreText)
                    .padding(.horizontal, 12)
                    .padding(.vertical, 10)
                Divider()
                    .padding(.leading, 36)
                statusRow(symbol: "point.3.connected.trianglepath.dotted", title: "NexusDock", value: nexusText)
                    .padding(.horizontal, 12)
                    .padding(.vertical, 10)
            }
            .background(
                RoundedRectangle(cornerRadius: 12, style: .continuous)
                    .fill(Color(nsColor: .controlBackgroundColor))
            )
            .overlay(
                RoundedRectangle(cornerRadius: 12, style: .continuous)
                    .stroke(Color(nsColor: .separatorColor).opacity(0.6), lineWidth: 0.5)
            )

            Button {
                if model.isUpdating {
                    onShowUpdateProgress()
                } else {
                    onOpenAgentDock()
                }
            } label: {
                Text(model.isUpdating
                     ? L10n.text("Show update progress")
                     : model.status.installed ? L10n.text("Open AgentDock") : L10n.text("Set up AgentDock…"))
                    .frame(maxWidth: .infinity)
            }
            .buttonStyle(.borderedProminent)
            .controlSize(.large)

            HStack(spacing: 8) {
                Button(serviceActionTitle) {
                    onRunServiceAction(serviceAction)
                }
                .frame(maxWidth: .infinity)
                .disabled(
                    model.isUpdating ||
                    !model.status.installed ||
                    model.status.requiresApproval
                )

                Button(L10n.text("Settings")) {
                    onOpenSettings()
                }
                .frame(maxWidth: .infinity)
                .disabled(model.isUpdating)
            }
            .controlSize(.regular)

            if let version = model.status.version, !version.isEmpty {
                Text("AgentDock \(version)")
                    .font(.system(size: 10.5))
                    .foregroundStyle(.tertiary)
                    .frame(maxWidth: .infinity)
            }
        }
        .padding(16)
        .frame(width: 350)
    }

    private func statusRow(symbol: String, title: String, value: String) -> some View {
        HStack(spacing: 10) {
            Image(systemName: symbol)
                .font(.system(size: 13))
                .foregroundStyle(.secondary)
                .frame(width: 14)
            Text(title)
                .font(.system(size: 12.5))
            Spacer()
            Text(value)
                .font(.system(size: 11.5, weight: .medium))
                .foregroundStyle(.secondary)
        }
    }
}
