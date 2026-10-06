import AppKit
import Foundation

@MainActor
final class AppDelegate: NSObject, NSApplicationDelegate {
    private let service = ServiceController()
    private let menuLoginAgent = MenuLoginAgentController()
    private let launchedInBackground = CommandLine.arguments.contains("--background")
    private var statusItem: NSStatusItem?
    private var currentStatus = ServiceStatus.missing
    private var timer: Timer?
    private var updateActivity: DesktopUpdateActivity = .idle
    private var trayServiceActionInProgress = false
    private lazy var updateProgressWindow = UpdateProgressWindowController()
    private lazy var setupWindow = NativeControlPanelWindowController(
        service: service,
        menuLoginAgent: menuLoginAgent,
        onChanged: { [weak self] in
            self?.refreshStatus()
        },
        onUpdateRequested: { [weak self] in
            self?.startUpdate(showCheckingPopover: false)
        }
    )
    private lazy var trayPopover = TrayPopoverController(
        onOpenAgentDock: { [weak self] in
            self?.showSetup()
        },
        onOpenSettings: { [weak self] in
            self?.showSettings()
        },
        onRunServiceAction: { [weak self] action in
            self?.runTrayPopoverServiceAction(action)
        },
        onShowUpdateProgress: { [weak self] in
            self?.showUpdateProgress()
        }
    )

    func applicationDidFinishLaunching(_ notification: Notification) {
        let recoveryInspection = DesktopUpdateTransactionRecovery.inspect(paths: service.paths)
        let pending = DesktopUpdateResult.load(from: service.paths.updateResult)
        if recoveryInspection.needsFinishingUI {
            setUpdateActivity(.applying)
            updateProgressWindow.presentFinishing(
                currentVersion: pending?.currentVersion ?? AppVersion.current,
                targetVersion: pending?.targetVersion ?? AppVersion.current
            )
        }

        startStatusTimer()
        Task { [weak self] in
            guard let self else { return }
            let outcome = await DesktopUpdateTransactionRecovery.recoverIfNeeded(
                paths: self.service.paths,
                inspection: recoveryInspection
            )
            self.finishLaunchingAfterUpdateRecovery(outcome: outcome)
        }
    }

    private func finishLaunchingAfterUpdateRecovery(
        outcome: DesktopUpdateTransactionRecovery.Outcome
    ) {
        var pendingUpdateResult = DesktopUpdateResult.load(from: service.paths.updateResult)
        var updateResultExists = FileManager.default.fileExists(atPath: service.paths.updateResult.path)
        var cleanCoordinationFiles = outcome.allowsCoordinationCleanup

        if outcome == .blocked {
            // unreadable journal、helper 失败或 recovery 期间 transaction 被替换时，都不能猜测成功。
            setUpdateActivity(.applying)
            updateProgressWindow.presentFinishing(
                currentVersion: pendingUpdateResult?.currentVersion ?? AppVersion.current,
                targetVersion: pendingUpdateResult?.targetVersion ?? AppVersion.current
            )
            refreshStatus()
            return
        }

        if let currentTransactionID = outcome.transactionID,
           let pendingTransactionID = pendingUpdateResult?.transactionID?
               .trimmingCharacters(in: .whitespacesAndNewlines),
           !pendingTransactionID.isEmpty,
           pendingTransactionID != currentTransactionID {
            if outcome.requiresUpdateLock {
                // active transaction 遇到其他 transaction 的 trigger 时不能擅自清理，保留现场等待 repair。
                NSLog("AgentDock update recovery: desktop trigger transaction does not match the active durable transaction.")
                setUpdateActivity(.applying)
                updateProgressWindow.presentFinishing(
                    currentVersion: pendingUpdateResult?.currentVersion ?? AppVersion.current,
                    targetVersion: pendingUpdateResult?.targetVersion ?? AppVersion.current
                )
                refreshStatus()
                return
            }

            NSLog("AgentDock found a stale desktop trigger for a different terminal transaction; discarding it.")
            let discarded = DesktopUpdateResult.discard(from: service.paths.updateResult)
            if discarded, cleanCoordinationFiles {
                DesktopUpdateServiceState.remove(at: service.paths.updateServiceState)
                DesktopUpdateHandoff.remove(at: service.paths.updateHandoff)
            } else if !discarded {
                cleanCoordinationFiles = false
            }
            pendingUpdateResult = nil
            updateResultExists = false
        }

        if case .noTransaction = outcome,
           let transactionID = pendingUpdateResult?.transactionID?
               .trimmingCharacters(in: .whitespacesAndNewlines),
           !transactionID.isEmpty {
            // transaction-aware trigger 没有对应 durable journal 时不能降级成 legacy 0.8.x。
            NSLog("AgentDock update recovery: transaction-aware desktop trigger has no durable transaction journal.")
            setUpdateActivity(.applying)
            updateProgressWindow.presentFinishing(
                currentVersion: pendingUpdateResult?.currentVersion ?? AppVersion.current,
                targetVersion: pendingUpdateResult?.targetVersion ?? AppVersion.current
            )
            refreshStatus()
            return
        }

        // 旧 0.8.x 更新结果没有 transaction id。若用户在更新完成后又手动替换/恢复了 App，
        // 结果文件记录的 target 已不再代表当前磁盘状态；继续按“更新收尾”处理只会永久锁住 UI。
        if let pendingResult = pendingUpdateResult,
           pendingResult.ok,
           pendingResult.transactionID?.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ?? true,
           AppVersion.display(pendingResult.targetVersion) != AppVersion.current {
            NSLog(
                "AgentDock found a legacy update result that no longer matches the active App; reconciling the current installation."
            )
            let discarded = DesktopUpdateResult.discard(from: service.paths.updateResult)
            if discarded, cleanCoordinationFiles {
                DesktopUpdateServiceState.remove(at: service.paths.updateServiceState)
                DesktopUpdateHandoff.remove(at: service.paths.updateHandoff)
            } else if !discarded {
                cleanCoordinationFiles = false
                NSLog("AgentDock could not discard the stale legacy update trigger; preserving update coordination files.")
            }
            pendingUpdateResult = nil
            // 当前 App 已明确不匹配 legacy target；即使文件系统暂时无法清理，也不能让旧 trigger 锁住本次启动。
            updateResultExists = false
        }

        if pendingUpdateResult == nil,
           updateResultExists,
           outcome.allowsStaleTriggerCleanup {
            // update-result.json 只是一次性启动触发器。durable transaction 已不存在恢复风险时，
            // malformed/stale trigger 不应让每次启动都重新进入 finishing UI。
            NSLog("AgentDock found a stale or malformed update result without recovery state; discarding the boot trigger.")
            let discarded = DesktopUpdateResult.discard(from: service.paths.updateResult)
            if discarded, cleanCoordinationFiles {
                DesktopUpdateServiceState.remove(at: service.paths.updateServiceState)
                DesktopUpdateHandoff.remove(at: service.paths.updateHandoff)
            } else if !discarded {
                cleanCoordinationFiles = false
                NSLog("AgentDock could not discard the stale update trigger; preserving update coordination files for a later retry.")
            }
            updateResultExists = false
        }

        if outcome.requiresUpdateLock && pendingUpdateResult == nil {
            // staged/trial/rolling_back 仍由 live Arbiter 持有，但 trigger 尚未生成或已损坏。
            // 保留全部协调文件，不能把 active transaction 降级成普通启动。
            setUpdateActivity(.applying)
            updateProgressWindow.presentFinishing(
                currentVersion: AppVersion.current,
                targetVersion: AppVersion.current
            )
            refreshStatus()
        } else if let pendingUpdateResult {
            setUpdateActivity(.applying)
            updateProgressWindow.presentFinishing(
                currentVersion: pendingUpdateResult.currentVersion,
                targetVersion: pendingUpdateResult.targetVersion
            )
            restoreBackgroundServicesAfterUpdate(pendingUpdateResult)
        } else if updateResultExists {
            // 只有 recovery 风险仍存在时，无法解析的 trigger 才能继续锁住启动。
            NSLog("AgentDock 更新结果存在但无法解析，保留后台服务事务状态等待恢复。")
            setUpdateActivity(.applying)
            updateProgressWindow.presentFinishing(
                currentVersion: AppVersion.current,
                targetVersion: AppVersion.current
            )
            refreshStatus()
        } else {
            finishNormalLaunch(cleanCoordinationFiles: cleanCoordinationFiles)
        }
    }

    private func finishNormalLaunch(cleanCoordinationFiles: Bool) {
        setUpdateActivity(.idle)
        configureMenuLoginAgentIfNeeded()
        if cleanCoordinationFiles {
            DesktopUpdateServiceState.remove(at: service.paths.updateServiceState)
            DesktopUpdateHandoff.remove(at: service.paths.updateHandoff)
        }
        refreshStatus(showWindow: !launchedInBackground)
        Task {
            _ = await service.reconcileBackgroundServicesOnLaunch()
            self.refreshStatus()
        }
    }

    func applicationWillTerminate(_ notification: Notification) {
        timer?.invalidate()
    }

    private func startStatusTimer() {
        guard timer == nil else { return }
        timer = Timer.scheduledTimer(withTimeInterval: 15, repeats: true) { [weak self] _ in
            Task { @MainActor in
                self?.refreshStatus()
            }
        }
    }

    private func setUpdateActivity(_ activity: DesktopUpdateActivity) {
        updateActivity = activity
        setStatusItemVisible(activity.statusItemVisible)
        ApplicationMenu.setQuitEnabled(!activity.locksApplication)
        setupWindow.setUpdateActivity(activity)
        refreshTrayPresentation()
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

                // Transitional safety for the first release that removes bundled cloudflared:
                // the source updater may predate the component store. During the target trial the
                // old App is still preserved in the rollback slot, so import its signed helper
                // before Tunnel registration is restored or the Arbiter is allowed to commit.
                let configuredMode = (try? service.configuredTunnelMode()) ?? .local
                try await service.migrateLegacyCloudflaredIfNeeded(
                    source: DesktopUpdateTransactionRecovery.legacyCloudflaredRollbackSource(paths: service.paths),
                    required: configuredMode != .local || serviceState.tunnelEnabled
                )

                // Restore Bundle-owned SMAppService definitions first. requiresApproval is an
                // explicit policy state and is reported to the Arbiter instead of failing the App.
                let registration = try await service.restoreBackgroundServiceRegistrationsForUpdate(
                    coreEnabled: serviceState.coreEnabled,
                    tunnelEnabled: serviceState.tunnelEnabled
                )
                // SMAppService.status == enabled does not prove launchd has actually started Core.
                // Perform one bounded health/self-heal pass before publishing the handoff so the
                // Arbiter only starts its final health/version gate after registration has settled.
                var warnings = await service.recoverBackgroundServicesAfterUpdate(
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

                if registration.core == "requires_approval" {
                    warnings.append(L10n.text("AgentDock Core needs background-item approval in System Settings."))
                }

                // Tunnel/public access is a soft dependency. Reconcile it best-effort, but surface
                // readiness only in logs and the control panel; it must not gate or decorate an
                // otherwise successful install/update result.
                _ = await service.reconcileConfiguredTunnel()

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
                        // failed 已经是 durable terminal state。保留 transaction/service-state
                        // 等 repair evidence，但消费 one-shot trigger，不能让 UI 永久伪装成 Updating。
                        _ = DesktopUpdateResult.discard(from: service.paths.updateResult)
                        presentTerminalUpdateFailure(
                            pendingResult: pendingResult,
                            terminalResult: terminalResult,
                            warnings: warnings,
                            keepUpdateLocked: false
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
            // transaction.json is the durable commit point. result.json is a projection for
            // desktop clients and may be missing if the Arbiter exits between the two atomic
            // writes. The transaction carries the same terminal fields, so consume it directly
            // instead of turning a completed update into a four-minute UI timeout.
            if let result = DesktopUpdateTerminalResult.load(
                from: service.paths.updateTransaction,
                transactionID: transactionID
            ) {
                return result
            }

            if Date() >= nextRecoveryProbe {
                // 正常更新时 source Arbiter 持有 transaction.lock，此探针立即无害返回；
                // 若 Arbiter 崩溃，则由同一 known-good source Arbiter 保守接管 rollback。
                _ = await DesktopUpdateTransactionRecovery.recoverIfNeeded(paths: service.paths)
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
            setUpdateActivity(.idle)
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

    private func setStatusItemVisible(_ visible: Bool) {
        if visible {
            guard statusItem == nil else { return }
            let item = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
            // NSStatusItem.visible 会按 autosaveName 持久化。更新阶段不能用 visible=false
            // 做临时隐藏，否则 App 在替换期间退出时会把“临时隐藏”永久写进用户偏好。
            item.autosaveName = "AgentDockMenuBarItem"
            item.isVisible = true
            if let button = item.button {
                button.image = AgentDockLogoArtwork.menuBarImage()
                button.toolTip = "AgentDock"
                button.target = self
                button.action = #selector(handleStatusItemClick(_:))
                button.sendAction(on: [.leftMouseUp, .rightMouseUp])
            }
            statusItem = item
            refreshTrayPresentation()
            return
        }

        guard let item = statusItem else { return }
        trayPopover.close()
        NSStatusBar.system.removeStatusItem(item)
        statusItem = nil
    }

    private func refreshStatus(showWindow: Bool = false) {
        Task {
            let status = await service.status()
            await MainActor.run {
                self.currentStatus = status
                self.refreshTrayPresentation()
                if showWindow {
                    self.setupWindow.present(status: status)
                } else if self.setupWindow.window?.isVisible == true {
                    self.setupWindow.refreshServiceStatus(status)
                }
            }
        }
    }

    func applicationShouldHandleReopen(_ sender: NSApplication, hasVisibleWindows flag: Bool) -> Bool {
        if updateActivity.locksApplication {
            updateProgressWindow.present()
            return true
        }
        setupWindow.present(status: currentStatus)
        return true
    }

    private func refreshTrayPresentation() {
        trayPopover.update(
            status: currentStatus,
            isUpdating: updateActivity.locksApplication,
            isCheckingForUpdate: updateActivity == .checking
        )
    }

    @objc private func handleStatusItemClick(_ sender: NSStatusBarButton) {
        guard let event = NSApp.currentEvent else { return }
        if event.type == .rightMouseUp {
            trayPopover.close()
            NSMenu.popUpContextMenu(makeTrayContextMenu(), with: event, for: sender)
            return
        }
        trayPopover.toggle(relativeTo: sender)
    }

    private func makeTrayContextMenu() -> NSMenu {
        let menu = NSMenu()
        // 从菜单栏按钮弹出时，NSMenu 会继承菜单栏自身的材质外观；
        // 明确使用 App 的有效外观，保留系统原生菜单效果，同时避免 Light 模式被渲染成黑玻璃。
        menu.appearance = NSApp.effectiveAppearance

        let state: String
        if currentStatus.healthy {
            state = L10n.text("Running")
        } else if currentStatus.loaded {
            state = L10n.text("Needs attention")
        } else {
            state = L10n.text("Stopped")
        }
        let version = AppVersion.display(currentStatus.version ?? AppVersion.current)
        let statusItem = NSMenuItem(title: "\(state) · \(version)", action: nil, keyEquivalent: "")
        statusItem.isEnabled = false
        menu.addItem(statusItem)

        menu.addItem(.separator())
        menu.addItem(item(L10n.text("Show main window"), #selector(showSetup)))

        menu.addItem(.separator())
        let startItem = item(L10n.text("Tray Start"), #selector(startService))
        startItem.isEnabled = !updateActivity.locksApplication
            && currentStatus.installed
            && !currentStatus.loaded
            && !currentStatus.requiresApproval
        menu.addItem(startItem)

        let restartItem = item(L10n.text("Tray Restart"), #selector(restartService))
        restartItem.isEnabled = !updateActivity.locksApplication
            && currentStatus.installed
            && !currentStatus.requiresApproval
        menu.addItem(restartItem)

        menu.addItem(.separator())
        menu.addItem(item(L10n.text("Settings"), #selector(showSettings)))

        let updateMenuItem = item(L10n.text("Tray Check for updates"), #selector(updateService))
        updateMenuItem.isEnabled = updateActivity.canCheckForUpdates
        menu.addItem(updateMenuItem)

        let exitItem = item(L10n.text("Exit AgentDock"), #selector(quit))
        exitItem.isEnabled = !updateActivity.locksApplication
        menu.addItem(exitItem)
        return menu
    }

    private func item(_ title: String, _ action: Selector) -> NSMenuItem {
        let menuItem = NSMenuItem(title: title, action: action, keyEquivalent: "")
        menuItem.target = self
        return menuItem
    }

    @objc private func showSetup() { setupWindow.present(status: currentStatus) }

    @objc private func showSettings() {
        setupWindow.presentSettings(status: currentStatus)
    }

    private func runTrayPopoverServiceAction(_ action: String) {
        if action == "start" {
            startService()
        } else {
            restartService()
        }
    }

    @objc private func showUpdateProgress() { updateProgressWindow.present() }
    @objc private func openPermissions() { setupWindow.presentPermissions() }
    @objc private func showActivity() { setupWindow.presentActivity(status: currentStatus) }
    @objc private func openLogs() { service.openLogs() }
    @objc private func openConfiguration() { service.openConfiguration() }
    @objc private func openBackgroundSettings() { service.openBackgroundItemsSettings() }

    @objc private func openDocumentation() {
        if let url = URL(string: "https://docs.nexusdock.co/agentdock/") {
            NSWorkspace.shared.open(url)
        }
    }

    @objc private func startService() {
        performServiceAction(L10n.text("Start"), recheckPublicEndpointOnSuccess: true) {
            _ = try await self.service.start()
        }
    }
    @objc private func stopService() { performServiceAction(L10n.text("Stop")) { try await self.service.stop() } }
    @objc private func restartService() {
        performServiceAction(L10n.text("Restart"), recheckPublicEndpointOnSuccess: true) {
            _ = try await self.service.restart()
        }
    }

    @objc private func updateService() {
        startUpdate(showCheckingPopover: true)
    }

    private func startUpdate(showCheckingPopover: Bool) {
        guard updateActivity.canCheckForUpdates else {
            if updateActivity == .checking {
                setupWindow.present(status: currentStatus)
            } else {
                updateProgressWindow.present()
            }
            return
        }
        guard !trayServiceActionInProgress, !setupWindow.hasActiveServiceOperation else {
            presentAlert(
                title: L10n.text("AgentDock is busy"),
                message: L10n.text("Wait for the current AgentDock operation to finish before starting an update.")
            )
            return
        }

        // “检查更新”只是只读查询，不应冻结服务、配置或退出；这里只禁止重复发起检查。
        setUpdateActivity(.checking)
        if showCheckingPopover,
           let button = statusItem?.button {
            // 右键菜单选择“检查更新”后菜单会立即消失；主动展示托盘面板，让用户能看到
            // “正在检查更新…”状态。延后一轮主线程，确保原生右键菜单已经完成 dismissal。
            DispatchQueue.main.async { [weak self] in
                self?.trayPopover.show(relativeTo: button)
            }
        }
        Task {
            do {
                let check = try await service.checkForUpdates()
                let shouldApply = await MainActor.run {
                    guard check.updateAvailable else {
                        self.setUpdateActivity(.idle)
                        self.presentAlert(
                            title: L10n.text("AgentDock is up to date"),
                            message: check.message
                        )
                        self.refreshStatus()
                        return false
                    }
                    guard self.confirmUpdate(check) else {
                        self.setUpdateActivity(.idle)
                        self.refreshStatus()
                        return false
                    }

                    // 检查期间允许服务和配置操作；真正进入更新事务前必须重新确认没有并发操作。
                    guard !self.trayServiceActionInProgress,
                          !self.setupWindow.hasActiveServiceOperation else {
                        self.setUpdateActivity(.idle)
                        self.presentAlert(
                            title: L10n.text("AgentDock is busy"),
                            message: L10n.text("Wait for the current AgentDock operation to finish before starting an update.")
                        )
                        self.refreshStatus()
                        return false
                    }

                    self.setUpdateActivity(.applying)
                    self.updateProgressWindow.presentChecking()
                    return true
                }
                guard shouldApply else { return }

                _ = try await service.applyUpdate { [weak self] event in
                    Task { @MainActor in
                        self?.updateProgressWindow.apply(event)
                    }
                }
                await MainActor.run {
                    self.setUpdateActivity(.idle)
                    self.refreshStatus()
                }
            } catch {
                await MainActor.run {
                    let failedWhileChecking = self.updateActivity == .checking
                    self.setUpdateActivity(.idle)
                    if failedWhileChecking {
                        self.presentAlert(
                            title: L10n.text("Check for updates"),
                            message: error.localizedDescription,
                            style: .warning
                        )
                    } else {
                        self.updateProgressWindow.showFailure(error.localizedDescription)
                    }
                    self.refreshStatus()
                }
            }
        }
    }

    private func confirmUpdate(_ check: DesktopUpdateCheck) -> Bool {
        let currentVersion = check.currentVersion ?? L10n.text("Unknown version")
        let latestVersion = check.latestVersion ?? L10n.text("Unknown version")
        let alert = NSAlert()
        alert.messageText = L10n.text("AgentDock Update")
        alert.informativeText = L10n.format(
            "A new AgentDock version is available.\n\nCurrent version: %@\nLatest version: %@\n\nUpdate now?",
            currentVersion,
            latestVersion
        )
        alert.alertStyle = .informational
        alert.addButton(withTitle: L10n.text("Update"))
        alert.addButton(withTitle: L10n.text("Cancel"))
        return alert.runModal() == .alertFirstButtonReturn
    }

    private func performServiceAction(
        _ action: String,
        recheckPublicEndpointOnSuccess: Bool = false,
        operation: @escaping () async throws -> Void
    ) {
        guard !updateActivity.locksApplication else {
            updateProgressWindow.present()
            return
        }
        guard !trayServiceActionInProgress, !setupWindow.hasActiveServiceOperation else { return }
        trayServiceActionInProgress = true
        Task {
            do {
                try await operation()
                try? await Task.sleep(nanoseconds: 800_000_000)
                await MainActor.run {
                    self.trayServiceActionInProgress = false
                    if recheckPublicEndpointOnSuccess {
                        self.setupWindow.invalidatePublicEndpointCheck()
                    }
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
                    self.refreshStatus()
                }
            }
        }
    }

    private func presentUpdateResult(_ result: DesktopUpdateResult, warning: String? = nil) {
        setUpdateActivity(.idle)
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
        guard !updateActivity.locksApplication else {
            updateProgressWindow.present()
            return
        }
        guard !trayServiceActionInProgress else { return }

        trayServiceActionInProgress = true
        Task {
            do {
                try await service.stop()
                await MainActor.run {
                    self.trayServiceActionInProgress = false
                    NSApp.terminate(nil)
                }
            } catch {
                await MainActor.run {
                    self.trayServiceActionInProgress = false
                    self.presentAlert(
                        title: L10n.text("Exit AgentDock"),
                        message: error.localizedDescription,
                        style: .warning
                    )
                }
            }
        }
    }
}
