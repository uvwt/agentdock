import Foundation
import ServiceManagement

enum BackgroundServiceTunnelDegradation: Equatable, Sendable {
    case coreUnavailable
    case componentUnavailable
    case registrationUnavailable
    case processUnavailable
}

enum BackgroundServiceTunnelState: Equatable, Sendable {
    case disabled
    case ready
    case degraded(BackgroundServiceTunnelDegradation)
}

struct BackgroundServiceLifecycleResult: Equatable, Sendable {
    let coreReady: Bool
    let tunnel: BackgroundServiceTunnelState
}

enum BackgroundServiceLifecyclePolicy {
    static func shouldKickstart(registrationEnabled: Bool, processID: Int?) -> Bool {
        registrationEnabled && processID == nil
    }

    static func shouldRunTunnel(mode: TunnelMode, coreReady: Bool, componentReady: Bool) -> Bool {
        mode != .local && coreReady && componentReady
    }
}

actor BackgroundServiceLifecycleCoordinator {
    private var tail = Task<Void, Never> {}

    /// 生命周期操作可能跨越 launchd 与健康检查等待。用任务链而不是 actor 方法本身
    /// 充当互斥边界，避免 actor 在 await 期间重入后同时修改 SMAppService。
    func run<T: Sendable>(
        _ operation: @escaping @Sendable () async throws -> T
    ) async throws -> T {
        let predecessor = tail
        let task = Task<T, Error> {
            await predecessor.value
            return try await operation()
        }
        tail = Task<Void, Never> {
            _ = try? await task.value
        }
        return try await task.value
    }
}

// MARK: - Lifecycle state machine

extension ServiceController {
    func reconcileBackgroundServicesOnLaunch() async -> BackgroundServiceLifecycleResult? {
        guard !LegacyDesktopRuntimeMigration.isPresent(paths: paths) else { return nil }
        do {
            return try await lifecycleCoordinator.run {
                switch self.coreService.status {
                case .enabled:
                    return try await self.startWithinLifecycle(forceCoreRestart: false)
                case .requiresApproval, .notRegistered, .notFound:
                    let mode = try self.configuredTunnelMode()
                    let tunnel = await self.reconcileTunnelWithinLifecycle(mode: mode, coreReady: false)
                    return BackgroundServiceLifecycleResult(coreReady: false, tunnel: tunnel)
                @unknown default:
                    return BackgroundServiceLifecycleResult(
                        coreReady: false,
                        tunnel: .degraded(.coreUnavailable)
                    )
                }
            }
        } catch {
            NSLog("AgentDock startup background-service reconciliation failed: %@", error.localizedDescription)
            return BackgroundServiceLifecycleResult(
                coreReady: false,
                tunnel: .degraded(.coreUnavailable)
            )
        }
    }

    func reconcileConfiguredTunnel() async -> BackgroundServiceTunnelState {
        do {
            return try await lifecycleCoordinator.run {
                let mode = try self.configuredTunnelMode()
                let coreReady = await self.coreReadyForCurrentApp(timeout: 1.5)
                return await self.reconcileTunnelWithinLifecycle(mode: mode, coreReady: coreReady)
            }
        } catch {
            NSLog("AgentDock configured Tunnel reconciliation failed: %@", error.localizedDescription)
            return .degraded(.registrationUnavailable)
        }
    }

    func startWithinLifecycle(forceCoreRestart: Bool) async throws -> BackgroundServiceLifecycleResult {
        let mode = try configuredTunnelMode()

        // Core 是 AgentDock 的基础能力，Tunnel 只是建立在 Core 之上的可选公网入口。
        // 修复 Core 前先收起旧 Tunnel，避免恢复窗口持续对外暴露 502。
        let coreAlreadyReady = forceCoreRestart
            ? false
            : await coreReadyForCurrentApp(timeout: 0.75)
        if !coreAlreadyReady {
            do {
                try setTunnelEnabledWithinLifecycle(false)
            } catch {
                NSLog("AgentDock could not quiesce Tunnel before Core recovery: %@", error.localizedDescription)
            }
        }

        try await ensureCoreReadyWithinLifecycle(forceRestart: forceCoreRestart)
        let tunnel = await reconcileTunnelWithinLifecycle(mode: mode, coreReady: true)
        return BackgroundServiceLifecycleResult(coreReady: true, tunnel: tunnel)
    }

    func stopWithinLifecycle() throws {
        var failures: [String] = []
        do {
            try setTunnelEnabledWithinLifecycle(false)
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

    private func ensureCoreReadyWithinLifecycle(forceRestart: Bool) async throws {
        guard let configuration = ServiceConfiguration.load(from: paths.environment) else {
            throw ValidationError(L10n.text("Configuration unavailable"))
        }

        if forceRestart {
            try reregister(service: coreService, label: Self.coreLabel, displayName: "AgentDock Core")
            try kickstartRegisteredService(
                label: Self.coreLabel,
                displayName: "AgentDock Core",
                killExisting: false
            )
        } else {
            try registerCoreIfNeeded()
        }

        if await coreReadyForCurrentApp(configuration: configuration, timeout: forceRestart ? 3 : 1.5) {
            return
        }

        let processID = launchdProcessID(label: Self.coreLabel)
        let registeredWithoutProcess = BackgroundServiceLifecyclePolicy.shouldKickstart(
            registrationEnabled: coreService.status == .enabled,
            processID: processID
        )
        let killExisting = !registeredWithoutProcess
        NSLog(
            killExisting
                ? "AgentDock Core is running but unhealthy or stale; kickstarting it once."
                : "AgentDock Core is registered but has no running process; kickstarting it once."
        )
        try kickstartRegisteredService(
            label: Self.coreLabel,
            displayName: "AgentDock Core",
            killExisting: killExisting
        )
        if await coreReadyForCurrentApp(configuration: configuration, timeout: 3) {
            return
        }

        // kickstart 只重启现有 launchd job；仍无法恢复时才重新绑定 Bundle registration。
        // 自动恢复到这里最多执行一次完整 re-register，避免形成无限自愈循环。
        NSLog("AgentDock Core did not recover after kickstart; re-registering it once.")
        try reregister(service: coreService, label: Self.coreLabel, displayName: "AgentDock Core")
        try kickstartRegisteredService(
            label: Self.coreLabel,
            displayName: "AgentDock Core",
            killExisting: false
        )
        guard await coreReadyForCurrentApp(configuration: configuration, timeout: 6) else {
            throw ValidationError(L10n.text("AgentDock background service is enabled, but the health check did not pass."))
        }
    }

    func reconcileTunnelWithinLifecycle(
        mode: TunnelMode,
        coreReady: Bool
    ) async -> BackgroundServiceTunnelState {
        guard coreReady else {
            do {
                try setTunnelEnabledWithinLifecycle(false)
            } catch {
                NSLog("AgentDock could not disable Tunnel while Core is unavailable: %@", error.localizedDescription)
                return .degraded(.registrationUnavailable)
            }
            return mode == .local ? .disabled : .degraded(.coreUnavailable)
        }

        guard mode != .local else {
            do {
                try setTunnelEnabledWithinLifecycle(false)
                return .disabled
            } catch {
                NSLog("AgentDock could not disable Tunnel for local mode: %@", error.localizedDescription)
                return .degraded(.registrationUnavailable)
            }
        }

        let component = await cloudflaredComponentStatus()
        guard BackgroundServiceLifecyclePolicy.shouldRunTunnel(
            mode: mode,
            coreReady: true,
            componentReady: component.ready
        ) else {
            // cloudflared 是显式可选组件。组件缺失或损坏时只降级公网入口，
            // 绝不能在后台偷偷安装，也不能让它阻断 Core。
            do {
                try setTunnelEnabledWithinLifecycle(false)
            } catch {
                NSLog("AgentDock could not disable Tunnel while its optional component is unavailable: %@", error.localizedDescription)
                return .degraded(.registrationUnavailable)
            }
            return .degraded(.componentUnavailable)
        }

        do {
            try setTunnelEnabledWithinLifecycle(true)
        } catch {
            NSLog("AgentDock Tunnel registration failed: %@", error.localizedDescription)
            return .degraded(.registrationUnavailable)
        }

        if launchdProcessID(label: Self.tunnelLabel) != nil {
            return .ready
        }

        do {
            try kickstartRegisteredService(
                label: Self.tunnelLabel,
                displayName: "AgentDock Tunnel",
                killExisting: false
            )
            if await waitForLaunchdPID(label: Self.tunnelLabel, timeout: 1.5) {
                return .ready
            }

            NSLog("AgentDock Tunnel did not recover after kickstart; re-registering it once.")
            try reregister(service: tunnelService, label: Self.tunnelLabel, displayName: "AgentDock Tunnel")
            try kickstartRegisteredService(
                label: Self.tunnelLabel,
                displayName: "AgentDock Tunnel",
                killExisting: false
            )
            if await waitForLaunchdPID(label: Self.tunnelLabel, timeout: 3) {
                return .ready
            }
        } catch {
            NSLog("AgentDock Tunnel automatic recovery failed: %@", error.localizedDescription)
            return .degraded(.registrationUnavailable)
        }
        return .degraded(.processUnavailable)
    }

    func coreReadyForCurrentApp(
        configuration: ServiceConfiguration? = nil,
        timeout: TimeInterval
    ) async -> Bool {
        guard coreService.status == .enabled,
              launchdProcessID(label: Self.coreLabel) != nil,
              let configuration = configuration ?? ServiceConfiguration.load(from: paths.environment),
              let healthURL = configuration.healthURL else {
            return false
        }

        let deadline = Date().addingTimeInterval(timeout)
        repeat {
            if let health = await fetchHealth(url: healthURL),
               health.ok,
               AppVersion.matchesHealthVersion(health.version) {
                return true
            }
            if Date() >= deadline {
                return false
            }
            try? await Task.sleep(nanoseconds: 150_000_000)
        } while !Task.isCancelled
        return false
    }
}
