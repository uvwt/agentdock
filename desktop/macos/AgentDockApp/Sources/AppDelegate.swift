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
    private var isUpdating = false
    private var trayServiceActionInProgress = false
    private lazy var updateProgressWindow = UpdateProgressWindowController()
    private lazy var setupWindow = SetupWindowController(
        service: service,
        menuLoginAgent: menuLoginAgent,
        onChanged: { [weak self] in
            self?.refreshStatus()
        },
        onUpdateRequested: { [weak self] in
            self?.startUpdate()
        }
    )

    func applicationDidFinishLaunching(_ notification: Notification) {
        let recoveryReady = DesktopUpdateTransactionRecovery.recoverIfNeeded(paths: service.paths)
        let pendingUpdateResult = DesktopUpdateResult.load(from: service.paths.updateResult)
        let updateResultExists = FileManager.default.fileExists(atPath: service.paths.updateResult.path)
        configureStatusItem()
        if !recoveryReady {
            // Do not acknowledge or clear any pending transaction when crash recovery itself
            // could not establish a safe state. The journal remains intact for repair/retry.
            setUpdateInProgress(true)
            updateProgressWindow.presentFinishing(
                currentVersion: pendingUpdateResult?.currentVersion ?? AppVersion.current,
                targetVersion: pendingUpdateResult?.targetVersion ?? AppVersion.current
            )
            refreshStatus()
        } else if let pendingUpdateResult {
            setUpdateInProgress(true)
            updateProgressWindow.presentFinishing(
                currentVersion: pendingUpdateResult.currentVersion,
                targetVersion: pendingUpdateResult.targetVersion
            )
            restoreBackgroundServicesAfterUpdate(pendingUpdateResult)
        } else if updateResultExists {
            // 结果文件存在但无法解析时，外部更新事务仍可能在等待新版 App ACK。
            // 保留 update-services.json，让外部更新器按超时路径恢复旧 App。
            NSLog("AgentDock 更新结果存在但无法解析，保留后台服务事务状态等待回滚。")
            setUpdateInProgress(true)
            updateProgressWindow.presentFinishing(
                currentVersion: AppVersion.current,
                targetVersion: AppVersion.current
            )
            refreshStatus()
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

    private func setUpdateInProgress(_ inProgress: Bool) {
        isUpdating = inProgress
        ApplicationMenu.setQuitEnabled(!inProgress)
        setupWindow.setUpdateInProgress(inProgress)
        rebuildMenu()
    }

    private func restoreBackgroundServicesAfterUpdate(_ pendingResult: DesktopUpdateResult) {
        Task {
            var handoffAcknowledged = false
            let transactionID = pendingResult.transactionID?
                .trimmingCharacters(in: .whitespacesAndNewlines)
            do {
                if pendingResult.ok,
                   AppVersion.display(pendingResult.targetVersion) != AppVersion.current {
                    throw ValidationError(L10n.format(
                        "The active AgentDock App version %@ does not match the update target %@.",
                        AppVersion.current,
                        AppVersion.display(pendingResult.targetVersion)
                    ))
                }
                guard let serviceState = try DesktopUpdateServiceState.load(from: service.paths.updateServiceState) else {
                    throw ValidationError(L10n.text("AgentDock update is missing background service recovery state."))
                }

                // Restore Bundle-owned SMAppService definitions first. requiresApproval is an
                // explicit policy state and is reported to the Arbiter instead of failing the App.
                let registration = try service.restoreBackgroundServiceRegistrationsForUpdate(
                    coreEnabled: serviceState.coreEnabled,
                    tunnelEnabled: serviceState.tunnelEnabled
                )
                if pendingResult.ok {
                    try DesktopUpdateHandoff(
                        targetVersion: pendingResult.targetVersion,
                        transactionID: pendingResult.transactionID,
                        coreRegistration: registration.core,
                        tunnelRegistration: registration.tunnel
                    ).write(to: service.paths.updateHandoff)
                    handoffAcknowledged = true
                } else if let transactionID = pendingResult.transactionID,
                          !transactionID.isEmpty {
                    // Rollback 也必须由恢复后的 source App 明确 ACK。这样 Arbiter 只有在
                    // SMAppService 已重新绑定回 source Bundle 后，才允许持久化 rolled_back。
                    try DesktopUpdateHandoff(
                        targetVersion: pendingResult.currentVersion,
                        transactionID: transactionID,
                        coreRegistration: registration.core,
                        tunnelRegistration: registration.tunnel
                    ).write(to: service.paths.updateHandoff)
                    handoffAcknowledged = true
                }

                let recoveryWarnings = await service.recoverBackgroundServicesAfterUpdate(
                    coreEnabled: serviceState.coreEnabled,
                    tunnelEnabled: serviceState.tunnelEnabled
                )

                // 更新事务恢复的是升级前的瞬时注册状态；公网 mode 才是 Tunnel 的长期意图。
                // 注册已恢复但服务仍在启动时只提示，不把正常的 macOS 启动延迟升级成回滚。
                var warnings = recoveryWarnings
                if registration.core == "requires_approval" {
                    warnings.append(L10n.text("AgentDock Core needs background-item approval in System Settings."))
                }
                if registration.tunnel == "requires_approval" {
                    warnings.append(L10n.text("AgentDock Tunnel needs background-item approval in System Settings."))
                }
                do {
                    try await service.reconcileTunnelRegistrationFromConfiguration()
                } catch {
                    warnings.append(L10n.format("Tunnel could not be restored for the current public access mode: %@", error.localizedDescription))
                    NSLog("AgentDock 更新后 Tunnel 状态收敛失败：%@", error.localizedDescription)
                }

                if let transactionID, !transactionID.isEmpty {
                    guard let terminalResult = await waitForUpdateTerminalResult(transactionID: transactionID) else {
                        // handoff 只证明当前 Bundle 能启动并恢复注册，不代表事务已经提交。
                        // 没有统一 terminal result 时保留 journal/rollback slot，禁止提前显示成功。
                        updateProgressWindow.showFailure(L10n.text(
                            "AgentDock update transaction did not report a final result. Recovery state was preserved."
                        ))
                        refreshStatus()
                        return
                    }
                    guard AppVersion.display(terminalResult.sourceVersion) == AppVersion.display(pendingResult.currentVersion),
                          AppVersion.display(terminalResult.targetVersion) == AppVersion.display(pendingResult.targetVersion) else {
                        updateProgressWindow.showFailure(L10n.text(
                            "AgentDock update transaction final result did not match the pending update. Recovery state was preserved."
                        ))
                        refreshStatus()
                        return
                    }
                    for warning in terminalResult.warnings ?? [] {
                        let value = warning.trimmingCharacters(in: .whitespacesAndNewlines)
                        if !value.isEmpty, !warnings.contains(value) {
                            warnings.append(value)
                        }
                    }

                    switch terminalResult.state {
                    case "committed":
                        guard pendingResult.ok,
                              AppVersion.display(terminalResult.targetVersion) == AppVersion.current else {
                            presentTerminalUpdateFailure(
                                pendingResult: pendingResult,
                                terminalResult: terminalResult,
                                warnings: warnings,
                                keepUpdateLocked: true
                            )
                            return
                        }
                        _ = DesktopUpdateResult.consume(from: service.paths.updateResult)
                        DesktopUpdateServiceState.remove(at: service.paths.updateServiceState)
                        if let menuLoginWarning = await restoreMenuLoginAgentAfterUpdateCommit() {
                            warnings.append(menuLoginWarning)
                        }
                        self.presentUpdateResult(
                            pendingResult,
                            warning: warnings.isEmpty ? nil : warnings.joined(separator: "\n")
                        )
                    case "rolled_back":
                        _ = DesktopUpdateResult.consume(from: service.paths.updateResult)
                        DesktopUpdateServiceState.remove(at: service.paths.updateServiceState)
                        presentTerminalUpdateFailure(
                            pendingResult: pendingResult,
                            terminalResult: terminalResult,
                            warnings: warnings,
                            keepUpdateLocked: false
                        )
                    case "failed":
                        // rollback 自身失败时保留 pending result/service-state；repair 仍需要这些证据。
                        presentTerminalUpdateFailure(
                            pendingResult: pendingResult,
                            terminalResult: terminalResult,
                            warnings: warnings,
                            keepUpdateLocked: true
                        )
                    default:
                        return
                    }
                    return
                }

                // 一次性兼容 pre-transaction 0.8.x。新架构只认 update/result.json 的最终状态。
                guard let result = DesktopUpdateResult.consume(from: service.paths.updateResult) else {
                    throw ValidationError(L10n.text("AgentDock update result was lost while restoring background services."))
                }
                DesktopUpdateServiceState.remove(at: service.paths.updateServiceState)
                if pendingResult.ok, let menuLoginWarning = await restoreMenuLoginAgentAfterUpdateCommit() {
                    warnings.append(menuLoginWarning)
                }
                self.presentUpdateResult(result, warning: warnings.isEmpty ? nil : warnings.joined(separator: "\n"))
            } catch {
                NSLog("AgentDock 更新后后台服务恢复失败：%@", error.localizedDescription)
                if let transactionID, !transactionID.isEmpty {
                    // transaction-aware 流程在 ACK/terminal 之前失败时必须保留 Updating 与 journal。
                    // 外部 Arbiter 会继续 rollback；若 rollback 本身失败，repair 仍有完整恢复证据。
                    updateProgressWindow.showFailure(error.localizedDescription)
                    refreshStatus()
                    return
                }
                if pendingResult.ok, handoffAcknowledged {
                    // 仅供 pre-transaction 0.8.x 兼容。新架构绝不把 handoff 当成最终成功。
                    self.presentUpdateResult(
                        pendingResult,
                        warning: L10n.format(
                            "Background services have not been restored yet. AgentDock will try again at the next launch: %@",
                            error.localizedDescription
                        )
                    )
                    return
                }
                if !pendingResult.ok {
                    self.presentUpdateResult(
                        pendingResult,
                        warning: L10n.format(
                            "The update process returned, but restoring background services failed: %@",
                            error.localizedDescription
                        )
                    )
                    return
                }
                self.refreshStatus()
            }
        }
    }

    private func waitForUpdateTerminalResult(
        transactionID: String,
        timeout: TimeInterval = 210
    ) async -> DesktopUpdateTerminalResult? {
        let deadline = Date().addingTimeInterval(timeout)
        var nextRecoveryProbe = Date().addingTimeInterval(5)
        while Date() < deadline {
            if let result = DesktopUpdateTerminalResult.load(
                from: service.paths.updateTerminalResult,
                transactionID: transactionID
            ) {
                return result
            }

            if Date() >= nextRecoveryProbe {
                let paths = service.paths
                await withCheckedContinuation { (continuation: CheckedContinuation<Void, Never>) in
                    DispatchQueue.global(qos: .utility).async {
                        // 正常更新时 source Arbiter 持有 transaction.lock，此探针立即无害返回；
                        // 若 Arbiter 崩溃，则由同一 known-good source Arbiter 保守接管 rollback。
                        _ = DesktopUpdateTransactionRecovery.recoverIfNeeded(paths: paths)
                        continuation.resume()
                    }
                }
                nextRecoveryProbe = Date().addingTimeInterval(5)
            }
            try? await Task.sleep(nanoseconds: 250_000_000)
        }
        return nil
    }

    private func presentTerminalUpdateFailure(
        pendingResult: DesktopUpdateResult,
        terminalResult: DesktopUpdateTerminalResult,
        warnings: [String],
        keepUpdateLocked: Bool
    ) {
        if !keepUpdateLocked {
            setUpdateInProgress(false)
        }
        var messages: [String] = []
        if !pendingResult.ok, !pendingResult.message.isEmpty {
            messages.append(pendingResult.message)
        }
        if let failure = terminalResult.failure?.message.trimmingCharacters(in: .whitespacesAndNewlines),
           !failure.isEmpty {
            messages.append(failure)
        }
        messages.append(contentsOf: warnings.filter { !$0.isEmpty })
        if messages.isEmpty {
            messages.append(L10n.text("Update failed"))
        }
        updateProgressWindow.showFailure(messages.joined(separator: "\n\n"))
        refreshStatus()
    }

    private func restoreMenuLoginAgentAfterUpdateCommit() async -> String? {
        if ProcessInfo.processInfo.environment["AGENTDOCK_SKIP_LOGIN_ITEM_CONFIGURATION"] == "1" {
            return nil
        }
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
        if isUpdating {
            updateProgressWindow.present()
            return true
        }
        setupWindow.present(status: currentStatus)
        return true
    }

    private func rebuildMenu() {
        let menu = NSMenu()
        if isUpdating {
            let statusMenuItem = NSMenuItem(
                title: L10n.format("AgentDock: %@", L10n.text("Updating…")),
                action: nil,
                keyEquivalent: ""
            )
            statusMenuItem.isEnabled = false
            menu.addItem(statusMenuItem)
            menu.addItem(.separator())
            menu.addItem(item(L10n.text("Show update progress"), #selector(showUpdateProgress)))
            if currentStatus.installed {
                menu.addItem(item(L10n.text("Open logs folder"), #selector(openLogs)))
            }
            statusItem.menu = menu
            return
        }

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
        menu.addItem(item(L10n.text("Check permissions"), #selector(openPermissions)))
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
    @objc private func showUpdateProgress() { updateProgressWindow.present() }
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
        startUpdate()
    }

    private func startUpdate() {
        guard !isUpdating else {
            updateProgressWindow.present()
            return
        }
        guard !trayServiceActionInProgress, !setupWindow.hasActiveServiceOperation else {
            presentAlert(
                title: L10n.text("AgentDock is busy"),
                message: L10n.text("Wait for the current AgentDock operation to finish before starting an update.")
            )
            return
        }
        setUpdateInProgress(true)
        updateProgressWindow.presentChecking()
        Task {
            do {
                _ = try await service.update { [weak self] event in
                    Task { @MainActor in
                        self?.updateProgressWindow.apply(event)
                    }
                }
                await MainActor.run {
                    self.setUpdateInProgress(false)
                    self.refreshStatus()
                }
            } catch {
                await MainActor.run {
                    self.setUpdateInProgress(false)
                    self.updateProgressWindow.showFailure(error.localizedDescription)
                    self.refreshStatus()
                }
            }
        }
    }

    private func performServiceAction(_ action: String, operation: @escaping () async throws -> Void) {
        guard !isUpdating else {
            updateProgressWindow.present()
            return
        }
        guard !trayServiceActionInProgress else { return }
        trayServiceActionInProgress = true
        Task {
            do {
                try await operation()
                try? await Task.sleep(nanoseconds: 800_000_000)
                await MainActor.run {
                    self.trayServiceActionInProgress = false
                    self.refreshStatus()
                }
            } catch {
                await MainActor.run {
                    self.trayServiceActionInProgress = false
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
        setUpdateInProgress(false)
        if result.ok {
            updateProgressWindow.showCompletion(targetVersion: result.targetVersion, warning: warning)
        } else {
            let message = [result.message, warning]
                .compactMap { $0 }
                .joined(separator: "\n\n")
            updateProgressWindow.showFailure(message)
        }
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
        guard !isUpdating else {
            updateProgressWindow.present()
            return
        }
        NSApp.terminate(nil)
    }
}
