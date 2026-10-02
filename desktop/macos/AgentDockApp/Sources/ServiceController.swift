import AppKit
import Foundation
import ServiceManagement

struct HealthPayload: Decodable {
    let ok: Bool
    let version: String
}

struct DesktopServiceStatusPayload: Decodable {
    let nexusConnected: Bool

    private enum CodingKeys: String, CodingKey {
        case nexusConnected = "nexus_connected"
    }
}

struct DesktopUpdateRegistrationState {
    let core: String
    let tunnel: String
}

enum BackgroundServiceLifecyclePolicy {
    static func shouldKickstart(registration: SMAppService.Status, processID: Int?) -> Bool {
        registration == .enabled && processID == nil
    }

    static func shouldRunTunnel(mode: TunnelMode, coreHealthy: Bool) -> Bool {
        coreHealthy && mode != .local
    }

    static func shouldUseFastStart(coreHealthy: Bool, tunnelMode _: TunnelMode, tunnelReady: Bool) -> Bool {
        coreHealthy && tunnelReady
    }

    static func shouldQuiesceTunnel(coreHealthy: Bool, tunnelRunning: Bool) -> Bool {
        !coreHealthy && tunnelRunning
    }

    static func kickstartArguments(target: String, killExisting: Bool) -> [String] {
        var arguments = ["kickstart"]
        if killExisting {
            arguments.append("-k")
        }
        arguments.append(target)
        return arguments
    }
}

private actor BackgroundServiceLifecycleGate {
    private var locked = false
    private var waiters: [CheckedContinuation<Void, Never>] = []

    func acquire() async {
        if !locked {
            locked = true
            return
        }
        await withCheckedContinuation { continuation in
            waiters.append(continuation)
        }
    }

    func release() {
        guard !waiters.isEmpty else {
            locked = false
            return
        }
        waiters.removeFirst().resume()
    }
}

enum NexusConnectionState: Equatable {
    case unconfigured
    case connected
    case disconnected
    case configurationError

    static func resolve(device: NexusDeviceStatus, connected: Bool) -> NexusConnectionState {
        if device.error != nil {
            return .configurationError
        }
        guard device.paired else {
            return .unconfigured
        }
        return connected ? .connected : .disconnected
    }
}

struct DesktopUpdateCheck: Decodable {
    let currentVersion: String?
    let latestVersion: String?
    let updateAvailable: Bool
    let message: String

    private enum CodingKeys: String, CodingKey {
        case currentVersion = "current_version"
        case latestVersion = "latest_version"
        case updateAvailable = "update_available"
        case message
    }

    static func decode(_ output: String) throws -> DesktopUpdateCheck {
        guard let data = output.data(using: .utf8) else {
            throw ValidationError(L10n.text("Unable to read the AgentDock update check result."))
        }
        do {
            return try JSONDecoder().decode(DesktopUpdateCheck.self, from: data)
        } catch {
            throw ValidationError(L10n.text("Unable to parse the AgentDock update check result."))
        }
    }
}

struct ServiceStatus {
    let installed: Bool
    let loaded: Bool
    let healthy: Bool
    let version: String?
    let configuration: ServiceConfiguration?
    let autostartEnabled: Bool
    let requiresApproval: Bool
    let migrationRequired: Bool
    let nexusConnection: NexusConnectionState

    static let missing = ServiceStatus(
        installed: false,
        loaded: false,
        healthy: false,
        version: nil,
        configuration: nil,
        autostartEnabled: false,
        requiresApproval: false,
        migrationRequired: false,
        nexusConnection: .unconfigured
    )
}

final class ServiceController: @unchecked Sendable {
    static let coreLabel = "com.uvwt.agentdock.core"
    static let tunnelLabel = "com.uvwt.agentdock.tunnel"
    static let corePlistName = "com.uvwt.agentdock.core.plist"
    static let tunnelPlistName = "com.uvwt.agentdock.tunnel.plist"

    let paths: AppPaths
    private let lifecycleGate = BackgroundServiceLifecycleGate()

    init(paths: AppPaths = AppPaths()) {
        self.paths = paths
    }

    func status() async -> ServiceStatus {
        let fileManager = FileManager.default
        let migrationRequired = LegacyDesktopRuntimeMigration.isPresent(paths: paths)
        let installed = fileManager.isExecutableFile(atPath: paths.binary.path)
            && fileManager.isExecutableFile(atPath: paths.cloudflared.path)
            && fileManager.fileExists(atPath: paths.coreSkillBundle.appendingPathComponent("manifest.json").path)
            && fileManager.fileExists(atPath: paths.environment.path)
        guard installed else { return .missing }

        let configuration = ServiceConfiguration.load(from: paths.environment)
        let nexusDevice = nexusDeviceStatus()
        let registration = coreService.status
        let requiresApproval = registration == .requiresApproval
        let enabled = registration == .enabled
        let registered = enabled || requiresApproval
        let loaded = enabled && isLoaded(label: Self.coreLabel)
        let nexusConnected = loaded && nexusDevice.paired ? await fetchNexusConnected() : false

        guard loaded, let healthURL = configuration?.healthURL else {
            return ServiceStatus(
                installed: true,
                loaded: loaded,
                healthy: false,
                version: nil,
                configuration: configuration,
                autostartEnabled: registered,
                requiresApproval: requiresApproval,
                migrationRequired: migrationRequired,
                nexusConnection: .resolve(device: nexusDevice, connected: nexusConnected)
            )
        }

        let health = await fetchHealth(url: healthURL)
        return ServiceStatus(
            installed: true,
            loaded: true,
            healthy: health?.ok == true,
            version: health?.version,
            configuration: configuration,
            autostartEnabled: registered,
            requiresApproval: requiresApproval,
            migrationRequired: migrationRequired,
            nexusConnection: .resolve(device: nexusDevice, connected: nexusConnected)
        )
    }

    func start() async throws {
        let startedAt = Date()
        logStartupTiming(action: "start", stage: "requested", startedAt: startedAt)
        await lifecycleGate.acquire()
        logStartupTiming(action: "start", stage: "gate_acquired", startedAt: startedAt)
        do {
            try await startLocked(startedAt: startedAt)
            await lifecycleGate.release()
        } catch {
            await lifecycleGate.release()
            throw error
        }
    }

    private func startLocked(startedAt: Date) async throws {
        let tunnelMode = try configuredTunnelMode()
        let configuration = ServiceConfiguration.load(from: paths.environment)
        let coreHealthy: Bool
        if let configuration,
           coreService.status == .enabled,
           !registrationVersionMismatch(label: Self.coreLabel),
           launchdProcessID(label: Self.coreLabel) != nil {
            coreHealthy = await waitForHealth(configuration: configuration, timeout: 0.75)
        } else {
            coreHealthy = false
        }
        let tunnelReady = tunnelReadyForFastStart(mode: tunnelMode)

        if BackgroundServiceLifecyclePolicy.shouldUseFastStart(
            coreHealthy: coreHealthy,
            tunnelMode: tunnelMode,
            tunnelReady: tunnelReady
        ) {
            logStartupTiming(action: "start", stage: "ready_fast_path", startedAt: startedAt)
            return
        }

        if BackgroundServiceLifecyclePolicy.shouldQuiesceTunnel(
            coreHealthy: coreHealthy,
            tunnelRunning: launchdProcessID(label: Self.tunnelLabel) != nil
        ) {
            try setTunnelEnabled(false)
            logStartupTiming(action: "start", stage: "tunnel_quiesced", startedAt: startedAt)
        }

        _ = try await ensureCoreHealthyForStart(startedAt: startedAt)
        logStartupTiming(action: "start", stage: "core_ready", startedAt: startedAt)
        try await restoreConfiguredTunnel(coreHealthy: true, startedAt: startedAt)
        logStartupTiming(action: "start", stage: "complete", startedAt: startedAt)
    }

    func stop() async throws {
        await lifecycleGate.acquire()
        do {
            try stopLocked()
            await lifecycleGate.release()
        } catch {
            await lifecycleGate.release()
            throw error
        }
    }

    private func stopLocked() throws {
        // "Stop AgentDock" means stop the whole externally reachable stack. Leaving the
        // Tunnel running after Core exits only exposes a persistent HTTP 502 endpoint.
        var failures: [String] = []
        do {
            try unregister(service: tunnelService, label: Self.tunnelLabel)
        } catch {
            failures.append("AgentDock Tunnel: \(error.localizedDescription)")
        }
        do {
            try unregister(service: coreService, label: Self.coreLabel)
        } catch {
            failures.append("AgentDock Core: \(error.localizedDescription)")
        }
        if !failures.isEmpty {
            throw ValidationError(failures.joined(separator: "\n"))
        }
    }

    func unregisterManagedBackgroundServicesForUninstall() throws {
        var failures: [String] = []
        do {
            try unregister(service: tunnelService, label: Self.tunnelLabel)
        } catch {
            failures.append("AgentDock Tunnel: \(error.localizedDescription)")
        }
        do {
            try unregister(service: coreService, label: Self.coreLabel)
        } catch {
            failures.append("AgentDock Core: \(error.localizedDescription)")
        }
        if !failures.isEmpty {
            throw ValidationError(failures.joined(separator: "\n"))
        }
    }

    func restart() async throws {
        await lifecycleGate.acquire()
        do {
            try await restartLocked()
            await lifecycleGate.release()
        } catch {
            await lifecycleGate.release()
            throw error
        }
    }

    private func restartLocked() async throws {
        try setTunnelEnabled(false)
        try reregister(service: coreService, label: Self.coreLabel, displayName: "AgentDock Core")
        try kickstartRegisteredService(label: Self.coreLabel, displayName: "AgentDock Core")
        guard await waitForLaunchdProcess(label: Self.coreLabel, timeout: 3),
              let configuration = ServiceConfiguration.load(from: paths.environment),
              await waitForHealth(configuration: configuration, timeout: 8) else {
            throw ValidationError(L10n.text("AgentDock Core was re-registered, but the health check did not pass."))
        }
        try await restoreConfiguredTunnel(coreHealthy: true)
    }

    func nexusDeviceStatus() -> NexusDeviceStatus {
        NexusDeviceStatus.load(from: paths.nexusDeviceIdentity)
    }

    func pairNexus(endpoint: String, pairingCode: String) async throws {
        let endpoint = endpoint.trimmingCharacters(in: .whitespacesAndNewlines)
        let pairingCode = pairingCode.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !endpoint.isEmpty, !pairingCode.isEmpty else {
            throw ValidationError(L10n.text("NexusDock address and one-time pairing code cannot be empty."))
        }
        let result = try await runInBackground {
            try runProcess(
                executable: self.paths.binary.path,
                arguments: ["nexus", "pair", "--endpoint", endpoint, "--code", pairingCode]
            )
        }
        guard result.status == 0 else {
            throw ValidationError(commandError(result.output, action: L10n.text("NexusDock pairing")))
        }
        try await restart()
    }

    func setAutostart(enabled: Bool) async throws {
        if enabled {
            try await start()
        } else {
            try await stop()
        }
    }

    func tunnelEnabled() -> Bool {
        let status = tunnelService.status
        return status == .enabled || status == .requiresApproval
    }

    func configuredTunnelMode() throws -> TunnelMode {
        guard FileManager.default.fileExists(atPath: paths.tunnelEnvironment.path) else {
            return .local
        }
        let environment = try ManagedEnvironment.load(from: paths.tunnelEnvironment)
        let rawMode = environment.values["AGENTDOCK_TUNNEL_MODE"]?
            .trimmingCharacters(in: .whitespacesAndNewlines)
            .lowercased() ?? ""
        return TunnelMode(rawValue: rawMode) ?? .local
    }

    func reconcileTunnelRegistrationFromConfiguration() throws {
        // 旧结构仍存在时必须先走迁移事务，不能在旁边提前注册第二套 Tunnel。
        guard !LegacyDesktopRuntimeMigration.isPresent(paths: paths) else { return }

        // 这里只收敛“是否应注册”的长期配置，不等待 cloudflared 或公网 ready。
        // 更新 handoff 已负责重新绑定目标 App；普通启动也不应因短暂网络状态重建 SMAppService。
        switch try configuredTunnelMode() {
        case .local:
            try setTunnelEnabled(false)
        case .quick, .named:
            try setTunnelEnabled(true)
        }
    }

    func reconcileBackgroundServicesOnLaunch() async {
        let startedAt = Date()
        logStartupTiming(action: "launch_reconcile", stage: "requested", startedAt: startedAt)
        await lifecycleGate.acquire()
        logStartupTiming(action: "launch_reconcile", stage: "gate_acquired", startedAt: startedAt)
        await reconcileBackgroundServicesOnLaunchLocked()
        logStartupTiming(action: "launch_reconcile", stage: "complete", startedAt: startedAt)
        await lifecycleGate.release()
    }

    private func reconcileBackgroundServicesOnLaunchLocked() async {
        guard !LegacyDesktopRuntimeMigration.isPresent(paths: paths) else { return }

        switch coreService.status {
        case .enabled:
            guard let configuration = ServiceConfiguration.load(from: paths.environment) else {
                NSLog("AgentDock startup reconciliation skipped because Core configuration is unavailable.")
                return
            }

            let registrationMismatch = registrationVersionMismatch(label: Self.coreLabel)
            let runningPID = launchdProcessID(label: Self.coreLabel)
            let quicklyHealthy = runningPID != nil
                ? await waitForHealth(configuration: configuration, timeout: 1.5)
                : false

            if registrationMismatch || !quicklyHealthy {
                // Keep the public Tunnel offline while repairing Core so an old Tunnel cannot
                // keep serving HTTP 502 during App replacement or launchd convergence.
                do {
                    try setTunnelEnabled(false)
                } catch {
                    NSLog("AgentDock could not disable Tunnel before Core repair: %@", error.localizedDescription)
                }
            }

            do {
                _ = try await ensureCoreHealthyForStart()
                try await restoreConfiguredTunnel(coreHealthy: true)
            } catch {
                NSLog("AgentDock startup background-service reconciliation failed: %@", error.localizedDescription)
            }

        case .requiresApproval, .notRegistered, .notFound:
            // A disabled/unapproved Core must not leave a public Tunnel forwarding to a dead
            // localhost origin. Preserve the user's Core disabled state and converge Tunnel off.
            do {
                try setTunnelEnabled(false)
            } catch {
                NSLog("AgentDock could not disable Tunnel while Core is unavailable: %@", error.localizedDescription)
            }

        @unknown default:
            NSLog("AgentDock startup reconciliation encountered an unknown Core registration state.")
        }
    }

    func setTunnelEnabled(_ enabled: Bool) throws {
        if enabled {
            try register(
                service: tunnelService,
                plistName: Self.tunnelPlistName,
                displayName: "AgentDock Tunnel"
            )
        } else {
            try unregister(service: tunnelService, label: Self.tunnelLabel)
        }
    }

    func restartTunnel() throws {
        try reregister(service: tunnelService, label: Self.tunnelLabel, displayName: "AgentDock Tunnel")
    }

    func restoreBackgroundServiceRegistrations(coreEnabled: Bool, tunnelEnabled: Bool) throws {
        if coreEnabled {
            try restoreRegistration(service: coreService, label: Self.coreLabel, displayName: "AgentDock Core")
        } else {
            try unregister(service: coreService, label: Self.coreLabel)
        }
        if tunnelEnabled {
            try restoreRegistration(service: tunnelService, label: Self.tunnelLabel, displayName: "AgentDock Tunnel")
        } else {
            try unregister(service: tunnelService, label: Self.tunnelLabel)
        }
    }

    func restoreBackgroundServiceRegistrationsForUpdate(
        coreEnabled: Bool,
        tunnelEnabled: Bool
    ) throws -> DesktopUpdateRegistrationState {
        let coreState = try restoreRegistrationForUpdate(
            service: coreService,
            label: Self.coreLabel,
            displayName: "AgentDock Core",
            expectedEnabled: coreEnabled
        )
        let tunnelState: String
        do {
            tunnelState = try restoreRegistrationForUpdate(
                service: tunnelService,
                label: Self.tunnelLabel,
                displayName: "AgentDock Tunnel",
                expectedEnabled: tunnelEnabled
            )
        } catch {
            // Tunnel availability depends on ServiceManagement policy plus external/network state.
            // Record the handoff state for diagnostics, but do not turn it into an update gate or
            // completion warning; the control panel owns eventual Tunnel/public readiness.
            NSLog("AgentDock Tunnel registration could not be restored during update handoff: %@", error.localizedDescription)
            tunnelState = "unavailable"
        }
        return DesktopUpdateRegistrationState(core: coreState, tunnel: tunnelState)
    }

    func recoverBackgroundServicesAfterUpdate(coreEnabled: Bool, tunnelEnabled: Bool) async -> [String] {
        // App Bundle 替换后，SMAppService 可能已经返回 enabled，但 launchd 尚未真正启动 Core。
        // 先给系统一个正常传播窗口；仍不健康时只做一次完整 unregister/register 自愈。
        // 最终更新是否提交仍由外部 Arbiter 的 Core health/version gate 决定。
        var warnings: [String] = []
        if tunnelEnabled,
           tunnelService.status == .enabled,
           !(await waitForTunnelProcess()) {
            NSLog("AgentDock Tunnel 注册显示 enabled 但进程未稳定，开始自动重新注册。")
            do {
                try restartTunnel()
                if !(await waitForTunnelProcess()) {
                    NSLog("AgentDock Tunnel 重新注册后进程仍未稳定。")
                }
            } catch {
                // Tunnel/public readiness is intentionally outside the update commit boundary.
                NSLog("AgentDock Tunnel 自动重新注册失败：%@", error.localizedDescription)
            }
        }
        if coreEnabled,
           coreService.status == .enabled,
           let configuration = ServiceConfiguration.load(from: paths.environment),
           !(await waitForHealth(configuration: configuration, timeout: 10)) {
            NSLog("AgentDock Core 注册显示 enabled 但健康检查未通过，开始自动重新注册。")
            do {
                try await restart()
            } catch {
                warnings.append(error.localizedDescription)
            }
        }
        return warnings
    }

    func reregisterBackgroundServices(coreEnabled: Bool, tunnelEnabled: Bool) async throws -> [String] {
        try restoreBackgroundServiceRegistrations(coreEnabled: coreEnabled, tunnelEnabled: tunnelEnabled)
        return await recoverBackgroundServicesAfterUpdate(coreEnabled: coreEnabled, tunnelEnabled: tunnelEnabled)
    }

    func openBackgroundItemsSettings() {
        SMAppService.openSystemSettingsLoginItems()
    }

    func waitForTunnelProcess(timeout: TimeInterval = 10) async -> Bool {
        await withCheckedContinuation { continuation in
            DispatchQueue.global(qos: .userInitiated).async {
                continuation.resume(returning: self.waitForStableLaunchdProcess(
                    label: Self.tunnelLabel,
                    timeout: timeout
                ))
            }
        }
    }

    func isLoaded() -> Bool {
        isLoaded(label: Self.coreLabel)
    }

    func waitForHealth(configuration: ServiceConfiguration, timeout: TimeInterval = 30) async -> Bool {
        await withCheckedContinuation { continuation in
            DispatchQueue.global(qos: .userInitiated).async {
                continuation.resume(returning: self.waitForHealthSynchronously(configuration: configuration, timeout: timeout))
            }
        }
    }

    func checkForUpdates() async throws -> DesktopUpdateCheck {
        try await runInBackground {
            let result = try runProcess(
                executable: self.paths.binary.path,
                arguments: ["update", "--check"],
                environment: ["AGENTDOCK_DESKTOP_APP_PATH": self.paths.appBundle.path]
            )
            guard result.status == 0 else {
                throw ValidationError(self.commandError(result.output, action: L10n.text("Check for updates")))
            }
            return try DesktopUpdateCheck.decode(result.output)
        }
    }

    func applyUpdate(onProgress: @escaping (UpdateProgressEvent) -> Void) async throws -> String {
        // 用户确认之后才检查后台服务写入能力并进入停服/替换阶段。
        // 纯版本检查不应该产生任何服务状态或更新事务副作用。
        try validateServiceManagementReadiness()

        let currentStatus = await status()
        let serviceState = DesktopUpdateServiceState(
            coreEnabled: currentStatus.autostartEnabled,
            tunnelEnabled: tunnelEnabled()
        )
        try serviceState.write(to: paths.updateServiceState)

        let output: String
        do {
            try setTunnelEnabled(false)
            try await stop()
            output = try await runInBackground {
                let result = try runUpdateProcess(
                    executable: self.paths.binary.path,
                    arguments: ["update", "--progress-json"],
                    environment: ["AGENTDOCK_DESKTOP_APP_PATH": self.paths.appBundle.path],
                    outputURL: self.paths.updateLog,
                    onProgress: onProgress
                )
                guard result.status == 0 else {
                    throw ValidationError(self.commandError(result.output, action: L10n.text("Update")))
                }
                return result.output.trimmingCharacters(in: .whitespacesAndNewlines)
            }
        } catch {
            let updateError = error
            do {
                let recoveryWarnings = try await reregisterBackgroundServices(
                    coreEnabled: serviceState.coreEnabled,
                    tunnelEnabled: serviceState.tunnelEnabled
                )
                if !recoveryWarnings.isEmpty {
                    NSLog("AgentDock 更新失败后后台服务仍在启动：%@", recoveryWarnings.joined(separator: "；"))
                }
                DesktopUpdateServiceState.remove(at: paths.updateServiceState)
            } catch {
                throw ValidationError(L10n.format(
                    "The update was not applied, and restoring background services also failed: %@; %@",
                    updateError.localizedDescription,
                    error.localizedDescription
                ))
            }
            throw updateError
        }

        // 真正的 App 替换会终止旧 GUI，并由新版 App 根据 update-result.json 恢复服务。
        // 能执行到这里说明更新进程正常返回但没有完成 GUI handoff，因此旧 GUI 必须自己收尾。
        do {
            let recoveryWarnings = try await reregisterBackgroundServices(
                coreEnabled: serviceState.coreEnabled,
                tunnelEnabled: serviceState.tunnelEnabled
            )
            if !recoveryWarnings.isEmpty {
                NSLog("AgentDock 更新返回后后台服务仍在启动：%@", recoveryWarnings.joined(separator: "；"))
            }
            DesktopUpdateServiceState.remove(at: paths.updateServiceState)
        } catch {
            throw ValidationError(L10n.format(
                "The update process returned, but restoring background services failed: %@",
                error.localizedDescription
            ))
        }
        return output
    }

    func openLogs() {
        try? FileManager.default.createDirectory(at: paths.logs, withIntermediateDirectories: true)
        NSWorkspace.shared.open(paths.logs)
    }

    func openConfiguration() {
        try? FileManager.default.createDirectory(at: paths.appSupport, withIntermediateDirectories: true)
        NSWorkspace.shared.open(paths.appSupport)
    }

    func openRuntimeAnalytics(configuration: ServiceConfiguration?) {
        guard let localMCPURL = configuration?.localMCPURL,
              var components = URLComponents(url: localMCPURL, resolvingAgainstBaseURL: false),
              components.scheme == "http",
              isLoopbackHost(components.host) else {
            return
        }
        components.path = "/analytics"
        components.query = nil
        components.fragment = nil
        guard let analyticsURL = components.url else { return }
        NSWorkspace.shared.open(analyticsURL)
    }

    private func isLoopbackHost(_ host: String?) -> Bool {
        guard let normalized = host?.lowercased() else { return false }
        return normalized == "localhost" || normalized == "127.0.0.1" || normalized == "::1"
    }

    private var coreService: SMAppService {
        SMAppService.agent(plistName: Self.corePlistName)
    }

    private var tunnelService: SMAppService {
        SMAppService.agent(plistName: Self.tunnelPlistName)
    }

    private func registerCoreIfNeeded() throws {
        try register(
            service: coreService,
            plistName: Self.corePlistName,
            displayName: "AgentDock Core"
        )
    }

    private func ensureCoreHealthyForStart(startedAt: Date? = nil) async throws -> ServiceConfiguration {
        try registerCoreIfNeeded()
        guard let configuration = ServiceConfiguration.load(from: paths.environment) else {
            throw ValidationError(L10n.text("Configuration unavailable"))
        }

        if registrationVersionMismatch(label: Self.coreLabel) {
            NSLog("AgentDock Core registration belongs to an older App bundle; re-registering it.")
            try reregister(service: coreService, label: Self.coreLabel, displayName: "AgentDock Core")
        }

        var didKickstart = false
        if BackgroundServiceLifecyclePolicy.shouldKickstart(
            registration: coreService.status,
            processID: launchdProcessID(label: Self.coreLabel)
        ) {
            NSLog("AgentDock Core is registered but has no running process; kickstarting it.")
            try kickstartRegisteredService(
                label: Self.coreLabel,
                displayName: "AgentDock Core",
                killExisting: false
            )
            didKickstart = true
        }

        var processReady = await waitForLaunchdPID(label: Self.coreLabel, timeout: 1.5)
        var coreHealthy = processReady
            ? await waitForHealth(configuration: configuration, timeout: 2)
            : false
        if coreHealthy {
            return configuration
        }

        if !didKickstart {
            NSLog("AgentDock Core is loaded but unhealthy; kickstarting it once before re-registration.")
            try kickstartRegisteredService(
                label: Self.coreLabel,
                displayName: "AgentDock Core",
                killExisting: true
            )
            processReady = await waitForLaunchdPID(label: Self.coreLabel, timeout: 2)
            coreHealthy = processReady
                ? await waitForHealth(configuration: configuration, timeout: 3)
                : false
            if coreHealthy {
                return configuration
            }
        }

        NSLog("AgentDock Core did not recover after kickstart; performing one automatic re-registration.")
        try reregister(service: coreService, label: Self.coreLabel, displayName: "AgentDock Core")
        try kickstartRegisteredService(
            label: Self.coreLabel,
            displayName: "AgentDock Core",
            killExisting: false
        )
        processReady = await waitForLaunchdPID(label: Self.coreLabel, timeout: 3)
        coreHealthy = processReady
            ? await waitForHealth(configuration: configuration, timeout: 6)
            : false

        guard coreHealthy else {
            throw ValidationError(L10n.text("AgentDock background service is enabled, but the health check did not pass."))
        }
        return configuration
    }

    private func restoreConfiguredTunnel(coreHealthy: Bool, startedAt: Date? = nil) async throws {
        let mode = try configuredTunnelMode()
        guard BackgroundServiceLifecyclePolicy.shouldRunTunnel(mode: mode, coreHealthy: coreHealthy) else {
            try setTunnelEnabled(false)
            logStartupTiming(action: "start", stage: "tunnel_disabled", startedAt: startedAt)
            return
        }

        if tunnelReadyForFastStart(mode: mode) {
            logStartupTiming(action: "start", stage: "tunnel_ready_existing", startedAt: startedAt)
            return
        }

        if tunnelService.status == .enabled,
           registrationVersionMismatch(label: Self.tunnelLabel) {
            NSLog("AgentDock Tunnel registration belongs to an older App bundle; re-registering it.")
            try restartTunnel()
        } else {
            try setTunnelEnabled(true)
        }

        var processReady = launchdProcessID(label: Self.tunnelLabel) != nil
        if !processReady {
            NSLog("AgentDock Tunnel is registered but has no running process; kickstarting it.")
            try kickstartRegisteredService(
                label: Self.tunnelLabel,
                displayName: "AgentDock Tunnel",
                killExisting: false
            )
            processReady = await waitForLaunchdPID(label: Self.tunnelLabel, timeout: 1.5)
        }

        if !processReady {
            NSLog("AgentDock Tunnel did not recover after kickstart; re-registering it once.")
            try restartTunnel()
            try kickstartRegisteredService(
                label: Self.tunnelLabel,
                displayName: "AgentDock Tunnel",
                killExisting: false
            )
            processReady = await waitForLaunchdPID(label: Self.tunnelLabel, timeout: 3)
        }

        if !processReady {
            NSLog("AgentDock Tunnel process did not appear after automatic recovery.")
        } else {
            logStartupTiming(action: "start", stage: "tunnel_process_ready", startedAt: startedAt)
        }
    }

    private func tunnelReadyForFastStart(mode: TunnelMode) -> Bool {
        switch mode {
        case .local:
            return tunnelService.status == .notRegistered || tunnelService.status == .notFound
        case .quick, .named:
            return tunnelService.status == .enabled
                && !registrationVersionMismatch(label: Self.tunnelLabel)
                && launchdProcessID(label: Self.tunnelLabel) != nil
        }
    }

    private func logStartupTiming(action: String, stage: String, startedAt: Date?) {
        guard let startedAt else { return }
        let elapsedMilliseconds = Date().timeIntervalSince(startedAt) * 1_000
        NSLog(
            "AgentDock startup timing action=%@ stage=%@ elapsed_ms=%.0f",
            action,
            stage,
            elapsedMilliseconds
        )
    }

    static func parentBundleVersion(fromLaunchctlOutput output: String) -> String? {
        for rawLine in output.split(whereSeparator: \.isNewline) {
            let line = rawLine.trimmingCharacters(in: .whitespaces)
            let prefix = "parent bundle version = "
            guard line.hasPrefix(prefix) else { continue }
            let value = line.dropFirst(prefix.count).trimmingCharacters(in: .whitespacesAndNewlines)
            return value.isEmpty ? nil : value
        }
        return nil
    }

    private func registrationVersionMismatch(label: String) -> Bool {
        guard let result = try? runProcess(
            executable: "/bin/launchctl",
            arguments: ["print", "\(serviceDomain)/\(label)"]
        ), result.status == 0,
        let registeredVersion = Self.parentBundleVersion(fromLaunchctlOutput: result.output) else {
            return false
        }
        return AppVersion.display(registeredVersion) != AppVersion.current
    }

    private func register(service: SMAppService, plistName: String, displayName: String) throws {
        try validateServiceManagementReadiness()
        try validateBundledServiceDefinition(plistName: plistName, displayName: displayName)
        switch service.status {
        case .enabled:
            return
        case .requiresApproval:
            throw ValidationError(L10n.format(
                "%@ is registered, but you need to allow it to run in the background in System Settings → General → Login Items & Extensions.",
                displayName
            ))
        case .notRegistered, .notFound:
            // SMAppService 在服务首次 register 前可能返回 .notFound，即使 Bundle 内 plist
            // 已经存在。定义是否完整由上面的 Bundle 文件校验负责，不用 status 猜测。
            try service.register()
        @unknown default:
            throw ValidationError(L10n.format("Unable to determine background service status for %@.", displayName))
        }
        if service.status == .requiresApproval {
            throw ValidationError(L10n.format(
                "%@ requires permission to run in the background in System Settings → General → Login Items & Extensions.",
                displayName
            ))
        }
        guard service.status == .enabled else {
            throw ValidationError(L10n.format(
                "%@ registration completed, but the system did not mark it as runnable.",
                displayName
            ))
        }
    }

    private func unregister(service: SMAppService, label: String) throws {
        switch service.status {
        case .notRegistered, .notFound:
            return
        case .enabled, .requiresApproval:
            try service.unregister()
            guard waitUntilUnregistered(service: service, label: label, timeout: 5) else {
                throw ValidationError(L10n.format(
                    "Background service %@ did not converge to the unregistered state.",
                    label
                ))
            }
        @unknown default:
            return
        }
    }

    private func reregister(service: SMAppService, label: String, displayName: String) throws {
        try unregister(service: service, label: label)
        let plistName = label == Self.coreLabel ? Self.corePlistName : Self.tunnelPlistName
        try register(service: service, plistName: plistName, displayName: displayName)
    }

    private func restoreRegistration(service: SMAppService, label: String, displayName: String) throws {
        try validateServiceManagementReadiness()
        let plistName = label == Self.coreLabel ? Self.corePlistName : Self.tunnelPlistName
        try validateBundledServiceDefinition(plistName: plistName, displayName: displayName)
        try unregister(service: service, label: label)
        try register(service: service, plistName: plistName, displayName: displayName)
    }

    private func restoreRegistrationForUpdate(
        service: SMAppService,
        label: String,
        displayName: String,
        expectedEnabled: Bool
    ) throws -> String {
        guard expectedEnabled else {
            try unregister(service: service, label: label)
            return "disabled"
        }
        do {
            try restoreRegistration(service: service, label: label, displayName: displayName)
        } catch {
            // requiresApproval reflects user/system policy. It is a commit warning, not evidence
            // that the newly installed App Bundle is invalid.
            guard service.status == .requiresApproval else { throw error }
        }
        switch service.status {
        case .enabled:
            return "enabled"
        case .requiresApproval:
            return "requires_approval"
        case .notRegistered, .notFound:
            throw ValidationError(L10n.format(
                "%@ background registration did not become available after the update.",
                displayName
            ))
        @unknown default:
            throw ValidationError(L10n.format(
                "%@ background registration returned an unknown state after the update.",
                displayName
            ))
        }
    }

    private var serviceDomain: String { "gui/\(getuid())" }

    func validatePersistentAppLocation() throws {
        let path = paths.appBundle.resolvingSymlinksInPath().path
        if path == "/Volumes" || path.hasPrefix("/Volumes/") {
            throw ValidationError(L10n.text("Move AgentDock to the Applications folder before enabling the background service."))
        }
    }

    func validateServiceManagementReadiness() throws {
        try validatePersistentAppLocation()
        if LegacyDesktopRuntimeMigration.isPresent(paths: paths) {
            throw ValidationError(L10n.text("A legacy AgentDock background layout was detected. Apply the current settings in the main panel to complete migration first."))
        }
    }

    func validateBundledServiceDefinition(plistName: String, displayName: String) throws {
        let plist = paths.appBundle
            .appendingPathComponent("Contents/Library/LaunchAgents", isDirectory: true)
            .appendingPathComponent(plistName)
        guard let values = try? plist.resourceValues(forKeys: [.isRegularFileKey, .isSymbolicLinkKey]),
              values.isRegularFile == true,
              values.isSymbolicLink != true else {
            throw ValidationError(L10n.format(
                "AgentDock.app is missing the background service definition for %@. Reinstall the application.",
                displayName
            ))
        }
    }

    private func launchdJobPresent(label: String) -> Bool {
        (try? runProcess(
            executable: "/bin/launchctl",
            arguments: ["print", "\(serviceDomain)/\(label)"]
        ).status) == 0
    }

    private func isLoaded(label: String) -> Bool {
        launchdProcessID(label: label) != nil
    }

    static func processID(fromLaunchctlOutput output: String) -> Int? {
        for rawLine in output.split(whereSeparator: \.isNewline) {
            let line = rawLine.trimmingCharacters(in: .whitespaces)
            guard line.hasPrefix("pid = "),
                  let pid = Int(line.dropFirst("pid = ".count)),
                  pid > 0 else { continue }
            return pid
        }
        return nil
    }

    private func launchdProcessID(label: String) -> Int? {
        guard let result = try? runProcess(
            executable: "/bin/launchctl",
            arguments: ["print", "\(serviceDomain)/\(label)"]
        ), result.status == 0 else { return nil }
        return Self.processID(fromLaunchctlOutput: result.output)
    }

    private func kickstartRegisteredService(
        label: String,
        displayName: String,
        killExisting: Bool = true
    ) throws {
        let arguments = BackgroundServiceLifecyclePolicy.kickstartArguments(
            target: "\(serviceDomain)/\(label)",
            killExisting: killExisting
        )
        let result = try runProcess(
            executable: "/bin/launchctl",
            arguments: arguments
        )
        guard result.status == 0 else {
            throw ValidationError(commandError(result.output, action: displayName))
        }
    }

    private func waitForLaunchdPID(label: String, timeout: TimeInterval) async -> Bool {
        await withCheckedContinuation { continuation in
            DispatchQueue.global(qos: .userInitiated).async {
                let deadline = Date().addingTimeInterval(timeout)
                while Date() < deadline {
                    if self.launchdProcessID(label: label) != nil {
                        continuation.resume(returning: true)
                        return
                    }
                    Thread.sleep(forTimeInterval: 0.1)
                }
                continuation.resume(returning: self.launchdProcessID(label: label) != nil)
            }
        }
    }

    private func waitForLaunchdProcess(label: String, timeout: TimeInterval) async -> Bool {
        await withCheckedContinuation { continuation in
            DispatchQueue.global(qos: .userInitiated).async {
                continuation.resume(returning: self.waitForStableLaunchdProcess(label: label, timeout: timeout))
            }
        }
    }

    private func waitForStableLaunchdProcess(label: String, timeout: TimeInterval) -> Bool {
        let deadline = Date().addingTimeInterval(timeout)
        var previousPID: Int?
        var stableChecks = 0
        while Date() < deadline {
            if let pid = launchdProcessID(label: label) {
                if pid == previousPID {
                    stableChecks += 1
                } else {
                    previousPID = pid
                    stableChecks = 1
                }
                if stableChecks >= 4 { return true }
            } else {
                previousPID = nil
                stableChecks = 0
            }
            Thread.sleep(forTimeInterval: 0.25)
        }
        return false
    }

    private func waitUntilUnregistered(service: SMAppService, label: String, timeout: TimeInterval) -> Bool {
        let deadline = Date().addingTimeInterval(timeout)
        while Date() < deadline {
            if !launchdJobPresent(label: label), Self.isUnregistered(service.status) { return true }
            Thread.sleep(forTimeInterval: 0.1)
        }
        return !launchdJobPresent(label: label) && Self.isUnregistered(service.status)
    }

    static func isUnregistered(_ status: SMAppService.Status) -> Bool {
        status == .notRegistered || status == .notFound
    }

    private func waitForHealthSynchronously(configuration: ServiceConfiguration, timeout: TimeInterval) -> Bool {
        guard let url = configuration.healthURL else { return false }
        let deadline = Date().addingTimeInterval(timeout)
        while Date() < deadline {
            let semaphore = DispatchSemaphore(value: 0)
            var healthy = false
            var request = URLRequest(url: url)
            request.timeoutInterval = 1.5
            URLSession.shared.dataTask(with: request) { data, response, _ in
                defer { semaphore.signal() }
                guard (response as? HTTPURLResponse)?.statusCode == 200,
                      let data,
                      let payload = try? JSONDecoder().decode(HealthPayload.self, from: data) else { return }
                healthy = payload.ok
            }.resume()
            _ = semaphore.wait(timeout: .now() + 2)
            if healthy { return true }
            Thread.sleep(forTimeInterval: 0.5)
        }
        return false
    }

    private func fetchNexusConnected() async -> Bool {
        do {
            let result = try await runInBackground {
                try runProcess(
                    executable: self.paths.binary.path,
                    arguments: ["service", "status", "--runtime-root", self.paths.appSupport.path]
                )
            }
            guard result.status == 0,
                  let data = result.output.data(using: .utf8),
                  let payload = try? JSONDecoder().decode(DesktopServiceStatusPayload.self, from: data) else {
                return false
            }
            return payload.nexusConnected
        } catch {
            return false
        }
    }

    private func fetchHealth(url: URL) async -> HealthPayload? {
        do {
            var request = URLRequest(url: url)
            request.timeoutInterval = 2.5
            let (data, response) = try await URLSession.shared.data(for: request)
            guard (response as? HTTPURLResponse)?.statusCode == 200 else { return nil }
            return try JSONDecoder().decode(HealthPayload.self, from: data)
        } catch {
            return nil
        }
    }

    private func commandError(_ output: String, action: String) -> String {
        let message = output.trimmingCharacters(in: .whitespacesAndNewlines)
        return message.isEmpty ? L10n.format("AgentDock %@ failed.", action) : message
    }

    func runInBackground<T>(_ operation: @escaping () throws -> T) async throws -> T {
        try await withCheckedThrowingContinuation { continuation in
            DispatchQueue.global(qos: .userInitiated).async {
                do { continuation.resume(returning: try operation()) }
                catch { continuation.resume(throwing: error) }
            }
        }
    }
}
