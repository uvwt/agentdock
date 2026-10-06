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

struct RuntimeExtensionOverview: Equatable {
    let available: Bool
    let skillCount: Int
    let pluginCount: Int
    let pluginsAvailable: Bool
    let mcpCount: Int

    static let unavailable = RuntimeExtensionOverview(
        available: false,
        skillCount: 0,
        pluginCount: 0,
        pluginsAvailable: false,
        mcpCount: 0
    )
}

private struct RuntimeOverviewCountPayload: Decodable {
    let count: Int
}

private struct RuntimeOverviewPluginsPayload: Decodable {
    let count: Int
    let available: Bool
}

private struct RuntimeOverviewPayload: Decodable {
    let skills: RuntimeOverviewCountPayload
    let plugins: RuntimeOverviewPluginsPayload
    let mcp: RuntimeOverviewCountPayload
}

struct DesktopUpdateRegistrationState: Sendable {
    let core: String
    let tunnel: String
}

private struct TunnelStatusPayload: Decodable {
    let mode: String
    let running: Bool
    let ready: Bool
    let publicURL: String?

    private enum CodingKeys: String, CodingKey {
        case mode, running, ready
        case publicURL = "public_url"
    }
}

struct CloudflaredComponentStatus: Decodable, Equatable, Sendable {
    let state: String
    let installed: Bool
    let ready: Bool
    let version: String?
    let detail: String?

    static let unavailable = CloudflaredComponentStatus(
        state: "broken",
        installed: false,
        ready: false,
        version: nil,
        detail: nil
    )
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

struct RuntimeDiagnosticCall: Decodable, Identifiable {
    let id: String
    let tool: String
    let source: String
    let startedAt: String
    let durationMS: Double
    let success: Bool
    let errorCode: String?

    private enum CodingKeys: String, CodingKey {
        case id, tool, source, success
        case startedAt = "started_at"
        case durationMS = "duration_ms"
        case errorCode = "error_code"
    }
}

struct RuntimeDiagnosticsPayload: Decodable {
    let recentCalls: [RuntimeDiagnosticCall]

    private enum CodingKeys: String, CodingKey {
        case recentCalls = "recent_calls"
    }
}

struct RuntimeAnalyticsStage: Decodable, Identifiable {
    let name: String
    let startedOffsetMS: Double
    let durationMS: Double
    let success: Bool

    var id: String { "\(name)-\(startedOffsetMS)-\(durationMS)" }

    private enum CodingKeys: String, CodingKey {
        case name, success
        case startedOffsetMS = "started_offset_ms"
        case durationMS = "duration_ms"
    }
}

struct RuntimeAnalyticsCall: Decodable, Identifiable {
    let id: UInt64
    let tool: String
    let source: String
    let startedAt: String
    let durationMS: Double
    let success: Bool
    let errorCode: String?
    let errorCategory: String?
    let stages: [RuntimeAnalyticsStage]

    private enum CodingKeys: String, CodingKey {
        case id, tool, source, success, stages
        case startedAt = "started_at"
        case durationMS = "duration_ms"
        case errorCode = "error_code"
        case errorCategory = "error_category"
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decode(UInt64.self, forKey: .id)
        tool = try container.decode(String.self, forKey: .tool)
        source = try container.decode(String.self, forKey: .source)
        startedAt = try container.decode(String.self, forKey: .startedAt)
        durationMS = try container.decode(Double.self, forKey: .durationMS)
        success = try container.decode(Bool.self, forKey: .success)
        errorCode = try container.decodeIfPresent(String.self, forKey: .errorCode)
        errorCategory = try container.decodeIfPresent(String.self, forKey: .errorCategory)
        stages = try container.decodeIfPresent([RuntimeAnalyticsStage].self, forKey: .stages) ?? []
    }
}

struct RuntimeToolStats: Decodable, Identifiable {
    let tool: String
    let count: Int
    let errorCount: Int
    let errorRate: Double
    let p50DurationMS: Double
    let p95DurationMS: Double
    let p99DurationMS: Double

    var id: String { tool }

    private enum CodingKeys: String, CodingKey {
        case tool, count
        case errorCount = "error_count"
        case errorRate = "error_rate"
        case p50DurationMS = "p50_duration_ms"
        case p95DurationMS = "p95_duration_ms"
        case p99DurationMS = "p99_duration_ms"
    }
}

struct RuntimeProcessSnapshot: Decodable {
    let goroutines: Int
    let heapAllocBytes: UInt64
    let heapInuseBytes: UInt64
    let heapSysBytes: UInt64
    let gcCycles: UInt32
    let uptimeMS: Int64

    private enum CodingKeys: String, CodingKey {
        case goroutines
        case heapAllocBytes = "heap_alloc_bytes"
        case heapInuseBytes = "heap_inuse_bytes"
        case heapSysBytes = "heap_sys_bytes"
        case gcCycles = "gc_cycles"
        case uptimeMS = "uptime_ms"
    }
}

struct RuntimeAnalyticsPayload: Decodable {
    let startedAt: String
    let recentCapacity: Int
    let windowCalls: Int
    let totalCalls: UInt64
    let totalErrors: UInt64
    let activeCalls: Int
    let toolStats: [RuntimeToolStats]
    let recentCalls: [RuntimeAnalyticsCall]
    let process: RuntimeProcessSnapshot

    private enum CodingKeys: String, CodingKey {
        case process
        case startedAt = "started_at"
        case recentCapacity = "recent_capacity"
        case windowCalls = "window_calls"
        case totalCalls = "total_calls"
        case totalErrors = "total_errors"
        case activeCalls = "active_calls"
        case toolStats = "tool_stats"
        case recentCalls = "recent_calls"
    }
}

struct RuntimeDashboardSnapshot {
    let countsAvailable: Bool
    let diagnosticsAvailable: Bool
    let skillCount: Int
    let mcpCount: Int
    let pluginCount: Int
    let recentCalls: [RuntimeDiagnosticCall]

    static let empty = RuntimeDashboardSnapshot(
        countsAvailable: false,
        diagnosticsAvailable: false,
        skillCount: 0,
        mcpCount: 0,
        pluginCount: 0,
        recentCalls: []
    )
}

final class ServiceController: @unchecked Sendable {
    static let coreLabel = "com.uvwt.agentdock.core"
    static let tunnelLabel = "com.uvwt.agentdock.tunnel"
    static let corePlistName = "com.uvwt.agentdock.core.plist"
    static let tunnelPlistName = "com.uvwt.agentdock.tunnel.plist"
    private static let quickTunnelReadyTimeout: TimeInterval = 85

    let paths: AppPaths
    let lifecycleCoordinator = BackgroundServiceLifecycleCoordinator()

    final class LifecycleTransaction: @unchecked Sendable {
        private let service: ServiceController

        fileprivate init(service: ServiceController) {
            self.service = service
        }

        func start() async throws -> BackgroundServiceLifecycleResult {
            try await service.startWithinLifecycle(forceCoreRestart: false)
        }

        func stop() throws {
            try service.stopWithinLifecycle()
        }

        func restart() async throws -> BackgroundServiceLifecycleResult {
            try await service.startWithinLifecycle(forceCoreRestart: true)
        }

        func setTunnelEnabled(_ enabled: Bool) throws {
            try service.setTunnelEnabledWithinLifecycle(enabled)
        }
    }

    init(paths: AppPaths = AppPaths()) {
        self.paths = paths
    }

    func withLifecycleTransaction<T: Sendable>(
        _ operation: @escaping @Sendable (LifecycleTransaction) async throws -> T
    ) async throws -> T {
        try await lifecycleCoordinator.run {
            try await operation(LifecycleTransaction(service: self))
        }
    }

    func status() async -> ServiceStatus {
        let fileManager = FileManager.default
        let migrationRequired = LegacyDesktopRuntimeMigration.isPresent(paths: paths)
        let installed = fileManager.isExecutableFile(atPath: paths.binary.path)
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

    func dashboard(configuration: ServiceConfiguration?) async -> RuntimeDashboardSnapshot {
        guard let configuration else { return .empty }

        async let overviewTask: RuntimeOverviewPayload? = fetchRuntimePayload(
            RuntimeOverviewPayload.self,
            configuration: configuration,
            path: "/internal/runtime/overview"
        )
        async let diagnosticsTask: RuntimeDiagnosticsPayload? = fetchRuntimePayload(
            RuntimeDiagnosticsPayload.self,
            configuration: configuration,
            path: "/internal/runtime/diagnostics"
        )
        let overviewPayload = await overviewTask

        let diagnosticsPayload = await diagnosticsTask
        return RuntimeDashboardSnapshot(
            countsAvailable: overviewPayload != nil,
            diagnosticsAvailable: diagnosticsPayload != nil,
            skillCount: overviewPayload?.skills.count ?? 0,
            mcpCount: overviewPayload?.mcp.count ?? 0,
            pluginCount: overviewPayload?.plugins.count ?? 0,
            recentCalls: diagnosticsPayload?.recentCalls ?? []
        )
    }

    func runtimeAnalytics(configuration: ServiceConfiguration?) async -> RuntimeAnalyticsPayload? {
        guard let configuration else { return nil }
        return await fetchRuntimePayload(
            RuntimeAnalyticsPayload.self,
            configuration: configuration,
            path: "/internal/runtime/analytics"
        )
    }

    func start() async throws -> BackgroundServiceLifecycleResult {
        try await withLifecycleTransaction { lifecycle in
            try await lifecycle.start()
        }
    }

    func stop() async throws {
        try await withLifecycleTransaction { lifecycle in
            try lifecycle.stop()
        }
    }

    func unregisterManagedBackgroundServicesForUninstall() throws {
        // 仅供 main.swift 的独立 --unregister-background-services helper 进程调用。
        // 该进程不会启动 AppDelegate，也不存在并发生命周期 mutation；正常 App 路径
        // 必须通过 lifecycleCoordinator，不能复用这个同步逃生口。
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

    func restart() async throws -> BackgroundServiceLifecycleResult {
        try await withLifecycleTransaction { lifecycle in
            try await lifecycle.restart()
        }
    }

    func nexusDeviceStatus() -> NexusDeviceStatus {
        NexusDeviceStatus.load(from: paths.nexusDeviceIdentity)
    }

    func runtimeExtensionOverview(configuration: ServiceConfiguration?) async -> RuntimeExtensionOverview {
        guard let configuration,
              let healthURL = configuration.healthURL,
              var components = URLComponents(url: healthURL, resolvingAgainstBaseURL: false) else {
            return .unavailable
        }
        components.path = "/internal/runtime/overview"
        components.query = nil
        components.fragment = nil
        guard let url = components.url else { return .unavailable }

        do {
            var request = URLRequest(url: url)
            request.timeoutInterval = 2.5
            if !configuration.authToken.isEmpty {
                request.setValue("Bearer \(configuration.authToken)", forHTTPHeaderField: "Authorization")
            }
            let (data, response) = try await URLSession.shared.data(for: request)
            guard (response as? HTTPURLResponse)?.statusCode == 200 else { return .unavailable }
            let payload = try JSONDecoder().decode(RuntimeOverviewPayload.self, from: data)
            return RuntimeExtensionOverview(
                available: true,
                skillCount: max(0, payload.skills.count),
                pluginCount: max(0, payload.plugins.count),
                pluginsAvailable: payload.plugins.available,
                mcpCount: max(0, payload.mcp.count)
            )
        } catch {
            return .unavailable
        }
    }

    func pairNexus(endpoint: String, pairingCode: String) async throws {
        let endpoint = endpoint.trimmingCharacters(in: .whitespacesAndNewlines)
        let pairingCode = pairingCode.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !endpoint.isEmpty, !pairingCode.isEmpty else {
            throw ValidationError(L10n.text("NexusDock address and one-time pairing code cannot be empty."))
        }
        try await withLifecycleTransaction { lifecycle in
            let result = try await self.runInBackground {
                try runProcess(
                    executable: self.paths.binary.path,
                    arguments: ["nexus", "pair", "--endpoint", endpoint, "--code", pairingCode]
                )
            }
            guard result.status == 0 else {
                throw ValidationError(self.commandError(result.output, action: L10n.text("NexusDock pairing")))
            }
            _ = try await lifecycle.restart()
        }
    }

    func setAutostart(enabled: Bool) async throws {
        if enabled {
            _ = try await start()
        } else {
            try await stop()
        }
    }

    func tunnelEnabled() -> Bool {
        let status = tunnelService.status
        return status == .enabled || status == .requiresApproval
    }

    func cloudflaredComponentStatus() async -> CloudflaredComponentStatus {
        do {
            let result = try await runInBackground {
                try runProcess(
                    executable: self.paths.binary.path,
                    arguments: [
                        "component", "status", "cloudflared",
                        "--runtime-root", self.paths.appSupport.path,
                        "--json",
                    ]
                )
            }
            guard result.status == 0,
                  let data = result.output.data(using: .utf8),
                  let status = try? JSONDecoder().decode(CloudflaredComponentStatus.self, from: data) else {
                return .unavailable
            }
            return status
        } catch {
            return .unavailable
        }
    }

    func migrateLegacyCloudflaredIfNeeded(source: URL?, required: Bool) async throws {
        try await lifecycleCoordinator.run {
            try await self.migrateLegacyCloudflaredWithinLifecycle(source: source, required: required)
        }
    }

    private func migrateLegacyCloudflaredWithinLifecycle(source: URL?, required: Bool) async throws {
        let current = await cloudflaredComponentStatus()
        guard !current.ready, required else { return }
        guard let source else {
            throw ValidationError(L10n.text("Cloudflare Tunnel is configured, but its optional component is missing. Repair the component before updating AgentDock."))
        }
        let values = try source.resourceValues(forKeys: [.isRegularFileKey, .isSymbolicLinkKey])
        guard values.isRegularFile == true, values.isSymbolicLink != true else {
            throw ValidationError(L10n.text("Cloudflare Tunnel is configured, but the legacy component cannot be migrated safely."))
        }
        let result = try await runInBackground {
            try runProcess(
                executable: self.paths.binary.path,
                arguments: [
                    "component", "__import-legacy", "cloudflared",
                    "--runtime-root", self.paths.appSupport.path,
                    "--source", source.path,
                    "--json",
                ]
            )
        }
        guard result.status == 0 else {
            throw ValidationError(L10n.text("Cloudflare Tunnel component migration failed. The AgentDock update was not committed."))
        }
        let migrated = await cloudflaredComponentStatus()
        guard migrated.ready else {
            throw ValidationError(L10n.text("Cloudflare Tunnel component migration did not produce a ready component."))
        }
    }

    func installCloudflaredComponent() async throws -> CloudflaredComponentStatus {
        try await lifecycleCoordinator.run {
            try await self.runCloudflaredComponentAction("install")
        }
    }

    func updateCloudflaredComponent() async throws -> CloudflaredComponentStatus {
        try await lifecycleCoordinator.run {
            try await self.runCloudflaredComponentAction("update")
        }
    }

    func uninstallCloudflaredComponent() async throws -> CloudflaredComponentStatus {
        try await lifecycleCoordinator.run {
            let mode = try self.configuredTunnelMode()
            if mode != .local {
                try self.setTunnelEnabledWithinLifecycle(false)
                try await self.configureTunnelWithinLifecycle(mode: .local, serverURL: "", tunnelToken: "")
            }
            return try await self.runCloudflaredComponentAction("uninstall")
        }
    }

    func configureTunnel(mode: TunnelMode, serverURL: String, tunnelToken: String) async throws {
        try await lifecycleCoordinator.run {
            try await self.configureTunnelWithinLifecycle(
                mode: mode,
                serverURL: serverURL,
                tunnelToken: tunnelToken
            )
        }
    }

    private func configureTunnelWithinLifecycle(
        mode: TunnelMode,
        serverURL: String,
        tunnelToken: String
    ) async throws {
        if mode != .local {
            let component = await cloudflaredComponentStatus()
            guard component.ready else {
                throw ValidationError(L10n.text("Install the Cloudflare Tunnel component first."))
            }
        }

        let wasEnabled = tunnelEnabled()
        if wasEnabled {
            try setTunnelEnabledWithinLifecycle(false)
        }

        let tokenFile = try writeTemporaryTunnelToken(tunnelToken)
        defer {
            if let tokenFile { try? FileManager.default.removeItem(at: tokenFile) }
        }

        var arguments = [
            "tunnel", "configure",
            "--runtime-root", paths.appSupport.path,
            "--mode", mode.rawValue,
            "--server-url", serverURL,
        ]
        if let tokenFile {
            arguments += ["--token-file", tokenFile.path]
        }

        do {
            let result = try await runInBackground {
                try runProcess(executable: self.paths.binary.path, arguments: arguments)
            }
            guard result.status == 0 else {
                throw ValidationError(commandError(result.output, action: L10n.text("Tunnel configuration")))
            }
            if mode != .local {
                try setTunnelEnabledWithinLifecycle(true)
            }
            if mode == .quick {
                try await waitForQuickTunnelReady()
            }
        } catch {
            if wasEnabled {
                try? setTunnelEnabledWithinLifecycle(true)
            }
            throw error
        }
    }

    private func waitForQuickTunnelReady() async throws {
        let deadline = Date().addingTimeInterval(Self.quickTunnelReadyTimeout)
        var sawReadyAddress = false

        while Date() < deadline {
            try Task.checkCancellation()

            let statusResult = try await runInBackground {
                try runProcess(
                    executable: self.paths.binary.path,
                    arguments: [
                        "tunnel", "status",
                        "--runtime-root", self.paths.appSupport.path,
                    ]
                )
            }
            if statusResult.status == 0,
               let data = statusResult.output.data(using: .utf8),
               let tunnelStatus = try? JSONDecoder().decode(TunnelStatusPayload.self, from: data),
               tunnelStatus.mode == TunnelMode.quick.rawValue,
               tunnelStatus.running,
               tunnelStatus.ready,
               let publicURL = tunnelStatus.publicURL?.trimmingCharacters(in: .whitespacesAndNewlines),
               !publicURL.isEmpty {
                sawReadyAddress = true

                if let configuration = ServiceConfiguration.load(from: paths.environment),
                   configuration.publicURL == publicURL,
                   await coreReadyForCurrentApp(configuration: configuration, timeout: 5) {
                    return
                }
            }

            try await Task.sleep(nanoseconds: 250_000_000)
        }

        if sawReadyAddress {
            throw ValidationError(L10n.text(
                "A temporary public address was generated, but AgentDock Core did not recover to a healthy state."
            ))
        }
        throw ValidationError(L10n.text(
            "cloudflared did not generate a temporary public address before the timeout."
        ))
    }

    func configuredNamedTunnelOrigin() -> String {
        let path = paths.appSupport.appendingPathComponent("named-server-url.txt")
        guard let data = try? Data(contentsOf: path),
              let value = String(data: data, encoding: .utf8)?.trimmingCharacters(in: .whitespacesAndNewlines) else {
            return ""
        }
        return value
    }

    private func runCloudflaredComponentAction(_ action: String) async throws -> CloudflaredComponentStatus {
        let result = try await runInBackground {
            try runProcess(
                executable: self.paths.binary.path,
                arguments: [
                    "component", action, "cloudflared",
                    "--runtime-root", self.paths.appSupport.path,
                    "--json",
                ]
            )
        }
        guard result.status == 0 else {
            throw ValidationError(L10n.text("Cloudflare Tunnel component operation failed. Check diagnostics and try again."))
        }
        guard let data = result.output.data(using: .utf8),
              let status = try? JSONDecoder().decode(CloudflaredComponentStatus.self, from: data) else {
            throw ValidationError(L10n.text("Unable to read Cloudflare Tunnel component status."))
        }
        return status
    }

    private func writeTemporaryTunnelToken(_ token: String) throws -> URL? {
        let token = token.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !token.isEmpty else { return nil }
        guard !token.contains("\n"), !token.contains("\r") else {
            throw ValidationError(L10n.text("Tunnel Token must be a single line of text."))
        }
        try FileManager.default.createDirectory(at: paths.appSupport, withIntermediateDirectories: true)
        let url = paths.appSupport.appendingPathComponent(".tunnel-token.\(UUID().uuidString)")
        try Data((token + "\n").utf8).write(to: url, options: .atomic)
        try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: url.path)
        return url
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

    func setTunnelEnabledWithinLifecycle(_ enabled: Bool) throws {
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

    private func restartTunnelWithinLifecycle() throws {
        try reregister(service: tunnelService, label: Self.tunnelLabel, displayName: "AgentDock Tunnel")
    }

    private func restoreBackgroundServiceRegistrationsWithinLifecycle(
        coreEnabled: Bool,
        tunnelEnabled: Bool
    ) throws {
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
    ) async throws -> DesktopUpdateRegistrationState {
        try await lifecycleCoordinator.run {
            try self.restoreBackgroundServiceRegistrationsForUpdateWithinLifecycle(
                coreEnabled: coreEnabled,
                tunnelEnabled: tunnelEnabled
            )
        }
    }

    private func restoreBackgroundServiceRegistrationsForUpdateWithinLifecycle(
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
        do {
            return try await lifecycleCoordinator.run {
                await self.recoverBackgroundServicesAfterUpdateWithinLifecycle(
                    coreEnabled: coreEnabled,
                    tunnelEnabled: tunnelEnabled
                )
            }
        } catch {
            return [error.localizedDescription]
        }
    }

    private func recoverBackgroundServicesAfterUpdateWithinLifecycle(
        coreEnabled: Bool,
        tunnelEnabled: Bool
    ) async -> [String] {
        // App Bundle 替换后，SMAppService 可能已经返回 enabled，但 launchd 尚未真正启动 Core。
        // 先给系统一个正常传播窗口；仍不健康时只做一次完整 unregister/register 自愈。
        // 最终更新是否提交仍由外部 Arbiter 的 Core health/version gate 决定。
        var warnings: [String] = []
        if tunnelEnabled,
           tunnelService.status == .enabled,
           !(await waitForTunnelProcess()) {
            NSLog("AgentDock Tunnel 注册显示 enabled 但进程未稳定，开始自动重新注册。")
            do {
                try restartTunnelWithinLifecycle()
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
           !(await coreReadyForCurrentApp(timeout: 10)) {
            NSLog("AgentDock Core 注册显示 enabled 但健康检查未通过，开始自动重新注册。")
            do {
                _ = try await startWithinLifecycle(forceCoreRestart: true)
            } catch {
                warnings.append(error.localizedDescription)
            }
        }
        return warnings
    }

    func reregisterBackgroundServices(coreEnabled: Bool, tunnelEnabled: Bool) async throws -> [String] {
        try await lifecycleCoordinator.run {
            try self.restoreBackgroundServiceRegistrationsWithinLifecycle(
                coreEnabled: coreEnabled,
                tunnelEnabled: tunnelEnabled
            )
            return await self.recoverBackgroundServicesAfterUpdateWithinLifecycle(
                coreEnabled: coreEnabled,
                tunnelEnabled: tunnelEnabled
            )
        }
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

    func applyUpdate(
        onProgress: @escaping @Sendable (UpdateProgressEvent) -> Void
    ) async throws -> String {
        try await lifecycleCoordinator.run {
            try await self.applyUpdateWithinLifecycle(onProgress: onProgress)
        }
    }

    private func applyUpdateWithinLifecycle(
        onProgress: @escaping @Sendable (UpdateProgressEvent) -> Void
    ) async throws -> String {
        // 用户确认之后才检查后台服务写入能力并进入停服/替换阶段。
        // 纯版本检查不应该产生任何服务状态或更新事务副作用。
        try validateServiceManagementReadiness()

        let currentStatus = await status()
        let serviceState = DesktopUpdateServiceState(
            coreEnabled: currentStatus.autostartEnabled,
            tunnelEnabled: tunnelEnabled()
        )
        let configuredMode = (try? configuredTunnelMode()) ?? .local
        try await migrateLegacyCloudflaredWithinLifecycle(
            source: paths.legacyBundledCloudflared,
            required: configuredMode != .local || serviceState.tunnelEnabled
        )
        try serviceState.write(to: paths.updateServiceState)

        let output: String
        do {
            try setTunnelEnabledWithinLifecycle(false)
            try stopWithinLifecycle()
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
                try restoreBackgroundServiceRegistrationsWithinLifecycle(
                    coreEnabled: serviceState.coreEnabled,
                    tunnelEnabled: serviceState.tunnelEnabled
                )
                let recoveryWarnings = await recoverBackgroundServicesAfterUpdateWithinLifecycle(
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
            try restoreBackgroundServiceRegistrationsWithinLifecycle(
                coreEnabled: serviceState.coreEnabled,
                tunnelEnabled: serviceState.tunnelEnabled
            )
            let recoveryWarnings = await recoverBackgroundServicesAfterUpdateWithinLifecycle(
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

    private func isLoopbackHost(_ host: String?) -> Bool {
        guard let normalized = host?.lowercased() else { return false }
        return normalized == "localhost" || normalized == "127.0.0.1" || normalized == "::1"
    }

    // 下面这些 module-internal 原语只供 BackgroundServiceLifecycle 的状态机编排；
    // UI、Installer 和更新流程不得绕过 lifecycleCoordinator 直接调用。
    var coreService: SMAppService {
        SMAppService.agent(plistName: Self.corePlistName)
    }

    var tunnelService: SMAppService {
        SMAppService.agent(plistName: Self.tunnelPlistName)
    }

    func registerCoreIfNeeded() throws {
        try register(
            service: coreService,
            plistName: Self.corePlistName,
            displayName: "AgentDock Core"
        )
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

    func unregister(service: SMAppService, label: String) throws {
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

    func reregister(service: SMAppService, label: String, displayName: String) throws {
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

    func launchdProcessID(label: String) -> Int? {
        guard let result = try? runProcess(
            executable: "/bin/launchctl",
            arguments: ["print", "\(serviceDomain)/\(label)"]
        ), result.status == 0 else { return nil }
        for rawLine in result.output.split(whereSeparator: \.isNewline) {
            let line = rawLine.trimmingCharacters(in: .whitespaces)
            guard line.hasPrefix("pid = "),
                  let pid = Int(line.dropFirst("pid = ".count)),
                  pid > 0 else { continue }
            return pid
        }
        return nil
    }

    func kickstartRegisteredService(
        label: String,
        displayName: String,
        killExisting: Bool
    ) throws {
        var arguments = ["kickstart"]
        if killExisting {
            arguments.append("-k")
        }
        arguments.append("\(serviceDomain)/\(label)")
        let result = try runProcess(executable: "/bin/launchctl", arguments: arguments)
        guard result.status == 0 else {
            throw ValidationError(commandError(result.output, action: displayName))
        }
    }

    func waitForLaunchdPID(label: String, timeout: TimeInterval) async -> Bool {
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

    func fetchHealth(url: URL) async -> HealthPayload? {
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

    private func fetchRuntimePayload<T: Decodable>(
        _ type: T.Type,
        configuration: ServiceConfiguration,
        path: String
    ) async -> T? {
        guard let localMCPURL = configuration.localMCPURL,
              var components = URLComponents(url: localMCPURL, resolvingAgainstBaseURL: false),
              components.scheme == "http",
              isLoopbackHost(components.host) else {
            return nil
        }
        components.path = path
        components.query = nil
        components.fragment = nil
        guard let url = components.url else { return nil }

        do {
            var request = URLRequest(url: url)
            request.timeoutInterval = 2.5
            if !configuration.authToken.isEmpty {
                request.setValue("Bearer \(configuration.authToken)", forHTTPHeaderField: "Authorization")
            }
            let (data, response) = try await URLSession.shared.data(for: request)
            guard (response as? HTTPURLResponse)?.statusCode == 200 else { return nil }
            return try JSONDecoder().decode(type, from: data)
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
