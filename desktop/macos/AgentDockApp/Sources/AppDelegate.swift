import AppKit
import Foundation

@MainActor
final class AppDelegate: NSObject, NSApplicationDelegate {
    private let service = ServiceController()
    private let menuLoginAgent = MenuLoginAgentController()
    private let launchedInBackground = CommandLine.arguments.contains("--background")
    private let statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
    private var currentStatus = ServiceStatus.missing
    private var timer: Timer?
    private lazy var setupWindow = SetupWindowController(
        service: service,
        menuLoginAgent: menuLoginAgent
    ) { [weak self] in
        self?.refreshStatus()
    }

    func applicationDidFinishLaunching(_ notification: Notification) {
        let pendingUpdateResult = DesktopUpdateResult.load(from: service.paths.updateResult)
        let updateResultExists = FileManager.default.fileExists(atPath: service.paths.updateResult.path)
        configureStatusItem()
        if let pendingUpdateResult {
            restoreBackgroundServicesAfterUpdate(pendingUpdateResult)
        } else if updateResultExists {
            // 结果文件存在但无法解析时，外部更新事务仍可能在等待新版 App ACK。
            // 保留 update-services.json，让外部更新器按超时路径恢复旧 App。
            NSLog("AgentDock 更新结果存在但无法解析，保留后台服务事务状态等待回滚。")
            refreshStatus(showWindow: !launchedInBackground)
        } else {
            // 没有 pending result 时，更新协调文件只能是上一次已结束流程留下的临时状态。
            configureMenuLoginAgentIfNeeded()
            DesktopUpdateServiceState.remove(at: service.paths.updateServiceState)
            DesktopUpdateHandoff.remove(at: service.paths.updateHandoff)
            refreshStatus(showWindow: !launchedInBackground)
            Task {
                do {
                    try await service.reconcileTunnelRegistrationFromConfiguration()
                } catch {
                    NSLog("AgentDock 启动时 Tunnel 状态收敛失败：%@", error.localizedDescription)
                }
                self.refreshStatus()
            }
        }
        timer = Timer.scheduledTimer(withTimeInterval: 15, repeats: true) { [weak self] _ in
            Task { @MainActor in
                self?.refreshStatus()
            }
        }
    }

    func applicationWillTerminate(_ notification: Notification) {
        timer?.invalidate()
    }

    private func restoreBackgroundServicesAfterUpdate(_ pendingResult: DesktopUpdateResult) {
        Task {
            var handoffAcknowledged = false
            do {
                guard let serviceState = try DesktopUpdateServiceState.load(from: service.paths.updateServiceState) else {
                    throw ValidationError(L10n.text("AgentDock update is missing background service recovery state."))
                }

                // 先恢复 SMAppService 注册，确认新版 Bundle 的后台服务定义可以被系统接受。
                // handoff ACK 随后立即发出；真正的 Core/Tunnel 进程 ready 仍由 launchd 异步完成，
                // 不能再用它们的启动速度决定是否回滚一个已经接管成功的新 App。
                try service.restoreBackgroundServiceRegistrations(
                    coreEnabled: serviceState.coreEnabled,
                    tunnelEnabled: serviceState.tunnelEnabled
                )
                if pendingResult.ok {
                    try DesktopUpdateHandoff(targetVersion: pendingResult.targetVersion).write(to: service.paths.updateHandoff)
                    handoffAcknowledged = true
                }

                let recoveryWarnings = await service.recoverBackgroundServicesAfterUpdate(
                    coreEnabled: serviceState.coreEnabled,
                    tunnelEnabled: serviceState.tunnelEnabled
                )
                guard let result = DesktopUpdateResult.consume(from: service.paths.updateResult) else {
                    throw ValidationError(L10n.text("AgentDock update result was lost while restoring background services."))
                }
                DesktopUpdateServiceState.remove(at: service.paths.updateServiceState)

                // 更新事务恢复的是升级前的瞬时注册状态；公网 mode 才是 Tunnel 的长期意图。
                // 注册已恢复但服务仍在启动时只提示，不把正常的 macOS 启动延迟升级成回滚。
                var warnings = recoveryWarnings
                do {
                    try await service.reconcileTunnelRegistrationFromConfiguration()
                } catch {
                    warnings.append(L10n.format("Tunnel could not be restored for the current public access mode: %@", error.localizedDescription))
                    NSLog("AgentDock 更新后 Tunnel 状态收敛失败：%@", error.localizedDescription)
                }
                if pendingResult.ok, let menuLoginWarning = await restoreMenuLoginAgentAfterUpdateCommit() {
                    warnings.append(menuLoginWarning)
                }
                self.presentUpdateResult(result, warning: warnings.isEmpty ? nil : warnings.joined(separator: "\n"))
            } catch {
                NSLog("AgentDock 更新后后台服务恢复失败：%@", error.localizedDescription)
                if pendingResult.ok, handoffAcknowledged {
                    // handoff 已经确认新版 App 本身可运行；保留 result/service-state，让下次启动
                    // 可以继续恢复后台服务，而不是把瞬时 SMAppService 故障变成整包回滚。
                    self.presentUpdateResult(
                        pendingResult,
                        warning: L10n.format(
                            "Background services have not been restored yet. AgentDock will try again at the next launch: %@",
                            error.localizedDescription
                        )
                    )
                } else if !pendingResult.ok {
                    self.presentAlert(
                        title: L10n.text("AgentDock recovery failed"),
                        message: error.localizedDescription,
                        style: .warning
                    )
                }
                self.refreshStatus()
            }
        }
    }

    private func restoreMenuLoginAgentAfterUpdateCommit() async -> String? {
        let fileManager = FileManager.default
        let deadline = Date().addingTimeInterval(10)
        while fileManager.fileExists(atPath: service.paths.updateHandoff.path), Date() < deadline {
            try? await Task.sleep(nanoseconds: 100_000_000)
        }
        guard !fileManager.fileExists(atPath: service.paths.updateHandoff.path) else {
            let message = L10n.text("Menu bar launch at sign-in will be reconciled at the next launch because the update transaction is still in progress.")
            NSLog("AgentDock 更新后菜单栏登录启动延后恢复：更新事务尚未完成。")
            return message
        }

        do {
            try menuLoginAgent.restoreAfterUpdate()
            return nil
        } catch {
            NSLog("AgentDock 更新后菜单栏登录启动恢复失败：%@", error.localizedDescription)
            return L10n.format("Menu bar launch at sign-in could not be restored: %@", error.localizedDescription)
        }
    }

    private func configureMenuLoginAgentIfNeeded() {
        if ProcessInfo.processInfo.environment["AGENTDOCK_SKIP_LOGIN_ITEM_CONFIGURATION"] == "1" {
            return
        }
        do {
            try menuLoginAgent.configureOnLaunch()
        } catch {
            // 菜单栏登录启动失败不影响 Core 后台服务；用户仍可手动打开 AgentDock。
            NSLog("AgentDock 菜单栏登录启动配置失败：%@", error.localizedDescription)
        }
    }

    private func configureStatusItem() {
        if let button = statusItem.button {
            button.image = NSImage(systemSymbolName: "shippingbox.fill", accessibilityDescription: "AgentDock")
            button.image?.isTemplate = true
        }
        rebuildMenu()
    }

    private func refreshStatus(showWindow: Bool = false) {
        Task {
            let status = await service.status()
            await MainActor.run {
                self.currentStatus = status
                self.rebuildMenu()
                if showWindow {
                    self.setupWindow.present(status: status)
                } else if self.setupWindow.window?.isVisible == true {
                    self.setupWindow.refreshServiceStatus(status)
                }
            }
        }
    }

    func applicationShouldHandleReopen(_ sender: NSApplication, hasVisibleWindows flag: Bool) -> Bool {
        setupWindow.present(status: currentStatus)
        return true
    }

    private func rebuildMenu() {
        let menu = NSMenu()
        let statusText: String
        if !currentStatus.installed {
            statusText = L10n.text("Not installed")
        } else if currentStatus.healthy {
            if AppVersion.matchesHealthVersion(currentStatus.version) {
                statusText = L10n.text("Running normally")
            } else {
                statusText = L10n.format(
                    "Version mismatch · AgentDock %@ · Core %@",
                    AppVersion.current,
                    AppVersion.display(currentStatus.version)
                )
            }
        } else if currentStatus.requiresApproval {
            statusText = L10n.text("Background permission required")
        } else if currentStatus.loaded {
            statusText = L10n.text("Service error")
        } else {
            statusText = L10n.text("Stopped")
        }
        let statusMenuItem = NSMenuItem(title: L10n.format("AgentDock: %@", statusText), action: nil, keyEquivalent: "")
        statusMenuItem.isEnabled = false
        menu.addItem(statusMenuItem)
        menu.addItem(.separator())

        menu.addItem(item(currentStatus.installed ? L10n.text("Open AgentDock") : L10n.text("Set up AgentDock…"), #selector(showSetup)))
        menu.addItem(item(L10n.text("Check permissions…"), #selector(openPermissions)))
        if currentStatus.installed {
            menu.addItem(.separator())

            if currentStatus.requiresApproval {
                menu.addItem(item(L10n.text("Open background settings"), #selector(openBackgroundSettings)))
            } else if currentStatus.loaded {
                menu.addItem(item(L10n.text("Stop AgentDock"), #selector(stopService)))
                menu.addItem(item(L10n.text("Restart AgentDock"), #selector(restartService)))
            } else {
                menu.addItem(item(L10n.text("Start AgentDock"), #selector(startService)))
            }
            menu.addItem(item(L10n.text("Check for updates…"), #selector(updateService)))
            menu.addItem(.separator())
            menu.addItem(item(L10n.text("Open logs folder"), #selector(openLogs)))
            menu.addItem(item(L10n.text("Open configuration folder"), #selector(openConfiguration)))
        }
        menu.addItem(item(L10n.text("Open documentation"), #selector(openDocumentation)))
        menu.addItem(.separator())
        menu.addItem(item(L10n.text("Exit menu bar app"), #selector(quit)))
        statusItem.menu = menu
    }

    private func item(_ title: String, _ action: Selector) -> NSMenuItem {
        let menuItem = NSMenuItem(title: title, action: action, keyEquivalent: "")
        menuItem.target = self
        return menuItem
    }

    @objc private func showSetup() { setupWindow.present(status: currentStatus) }
    @objc private func openPermissions() { setupWindow.presentPermissions() }
    @objc private func openLogs() { service.openLogs() }
    @objc private func openConfiguration() { service.openConfiguration() }
    @objc private func openBackgroundSettings() { service.openBackgroundItemsSettings() }

    @objc private func openDocumentation() {
        if let url = URL(string: "https://uvwt.github.io/agentdock-docs/") {
            NSWorkspace.shared.open(url)
        }
    }

    @objc private func startService() { performServiceAction(L10n.text("Start")) { try await self.service.start() } }
    @objc private func stopService() { performServiceAction(L10n.text("Stop")) { try await self.service.stop() } }
    @objc private func restartService() { performServiceAction(L10n.text("Restart")) { try await self.service.restart() } }

    @objc private func updateService() {
        Task {
            do {
                let output = try await service.update()
                await MainActor.run {
                    self.presentAlert(
                        title: L10n.text("AgentDock update completed"),
                        message: output.isEmpty ? L10n.text("Update completed.") : output
                    )
                    self.refreshStatus()
                }
            } catch {
                await MainActor.run {
                    self.presentAlert(title: L10n.text("Update failed"), message: error.localizedDescription, style: .warning)
                }
            }
        }
    }

    private func performServiceAction(_ action: String, operation: @escaping () async throws -> Void) {
        Task {
            do {
                try await operation()
                try? await Task.sleep(nanoseconds: 800_000_000)
                await MainActor.run { self.refreshStatus() }
            } catch {
                await MainActor.run {
                    self.presentAlert(
                        title: L10n.format("%@ failed", action),
                        message: error.localizedDescription,
                        style: .warning
                    )
                }
            }
        }
    }

    private func presentUpdateResult(_ result: DesktopUpdateResult, warning: String? = nil) {
        NSApp.activate(ignoringOtherApps: true)
        let title = result.ok ? L10n.text("AgentDock update completed") : L10n.text("AgentDock update failed")
        let message = [result.message, warning]
            .compactMap { $0 }
            .joined(separator: "\n\n")
        presentAlert(title: title, message: message, style: result.ok ? .informational : .warning)
        refreshStatus()
    }

    private func presentAlert(title: String, message: String, style: NSAlert.Style = .informational) {
        let alert = NSAlert()
        alert.messageText = title
        alert.informativeText = message
        alert.alertStyle = style
        alert.runModal()
    }

    @objc private func quit() {
        NSApp.terminate(nil)
    }
}
