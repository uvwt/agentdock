import Foundation
import ServiceManagement

@main
struct ServiceControllerValidationTests {
    static func main() throws {
        let root = FileManager.default.temporaryDirectory
            .appendingPathComponent("AgentDockServiceValidationTests-\(UUID().uuidString)", isDirectory: true)
        defer { try? FileManager.default.removeItem(at: root) }

        let launchAgents = root.appendingPathComponent("Library/LaunchAgents", isDirectory: true)
        try FileManager.default.createDirectory(at: launchAgents, withIntermediateDirectories: true)
        try Data("legacy".utf8).write(
            to: launchAgents.appendingPathComponent("com.uvwt.agentdock.plist")
        )

        let appBundle = root.appendingPathComponent("Applications/AgentDock.app", isDirectory: true)
        let launchAgentBundle = appBundle
            .appendingPathComponent("Contents/Library/LaunchAgents", isDirectory: true)
        try FileManager.default.createDirectory(at: launchAgentBundle, withIntermediateDirectories: true)
        let corePlist = launchAgentBundle.appendingPathComponent(ServiceController.corePlistName)
        try Data("plist".utf8).write(to: corePlist)

        let persistentPaths = AppPaths(
            home: root,
            appBundle: appBundle
        )
        let service = ServiceController(paths: persistentPaths)

        // 迁移入口必须允许旧结构存在，否则在 begin() 之前就会被自己拦住。
        try service.validatePersistentAppLocation()
        expectFailure("检测到旧版") {
            try service.validateServiceManagementReadiness()
        }
        try service.validateBundledServiceDefinition(
            plistName: ServiceController.corePlistName,
            displayName: "AgentDock Core"
        )
        try FileManager.default.removeItem(at: corePlist)
        expectFailure("缺少 AgentDock Core") {
            try service.validateBundledServiceDefinition(
                plistName: ServiceController.corePlistName,
                displayName: "AgentDock Core"
            )
        }

        let mountedPaths = AppPaths(
            home: root,
            appBundle: URL(fileURLWithPath: "/Volumes/AgentDock/AgentDock.app", isDirectory: true)
        )
        expectFailure("应用程序") {
            try ServiceController(paths: mountedPaths).validatePersistentAppLocation()
        }

        try testConfiguredTunnelMode(root: root, appBundle: appBundle)
        testQuickTunnelBootstrap()
        testServiceRegistrationStatusClassification()
        try testNexusConnectionStateResolution(root: root)
        try testDesktopUpdateCheckDecoding()

        print("service controller validation tests passed")
    }

    private static func testConfiguredTunnelMode(root: URL, appBundle: URL) throws {
        let home = root.appendingPathComponent("tunnel-mode-home", isDirectory: true)
        let paths = AppPaths(home: home, appBundle: appBundle)
        let service = ServiceController(paths: paths)

        let missingMode = try service.configuredTunnelMode()
        precondition(missingMode == .local)
        try FileManager.default.createDirectory(at: paths.appSupport, withIntermediateDirectories: true)

        for (rawMode, expected) in [
            ("none", TunnelMode.local),
            ("quick", TunnelMode.quick),
            ("named", TunnelMode.named),
            ("unexpected", TunnelMode.local),
        ] {
            try Data("AGENTDOCK_TUNNEL_MODE='\(rawMode)'\n".utf8).write(to: paths.tunnelEnvironment)
            let configuredMode = try service.configuredTunnelMode()
            precondition(configuredMode == expected, rawMode)
        }
    }

    private static func testQuickTunnelBootstrap() {
        var values = [
            "AGENTDOCK_AUTH_TOKEN": "token",
            "AGENTDOCK_OAUTH_PASSWORD": "password",
            "AGENTDOCK_OAUTH_TOKEN_SECRET": "secret",
            "AGENTDOCK_SERVER_URL": "https://stale.trycloudflare.com",
            "AGENTDOCK_OAUTH_ENABLED": "true",
        ]

        QuickTunnelBootstrap.prepareInitialCoreEnvironment(&values)

        precondition(values["AGENTDOCK_SERVER_URL"] == nil)
        precondition(values["AGENTDOCK_OAUTH_ENABLED"] == "false")
        precondition(values["AGENTDOCK_AUTH_TOKEN"] == "token")
        precondition(values["AGENTDOCK_OAUTH_PASSWORD"] == "password")
        precondition(values["AGENTDOCK_OAUTH_TOKEN_SECRET"] == "secret")
    }

    private static func testServiceRegistrationStatusClassification() {
        precondition(ServiceController.isUnregistered(.notRegistered))
        precondition(ServiceController.isUnregistered(.notFound))
        precondition(!ServiceController.isUnregistered(.enabled))
        precondition(!ServiceController.isUnregistered(.requiresApproval))
    }

    private static func testNexusConnectionStateResolution(root: URL) throws {
        let identityURL = root.appendingPathComponent("nexus-device.json")
        try Data(#"{"endpoint":"https://nexus.example.com","node_id":"node_test","device_id":"device_test","device_token":"secret"}"#.utf8)
            .write(to: identityURL)
        let paired = NexusDeviceStatus.load(from: identityURL)

        let connectedStatus = try JSONDecoder().decode(
            DesktopServiceStatusPayload.self,
            from: Data(#"{"nexus_connected":true}"#.utf8)
        )
        precondition(connectedStatus.nexusConnected)
        precondition(NexusConnectionState.resolve(device: paired, connected: connectedStatus.nexusConnected) == .connected)
        precondition(NexusConnectionState.resolve(device: paired, connected: false) == .disconnected)

        let missing = NexusDeviceStatus.load(from: root.appendingPathComponent("missing-device.json"))
        precondition(NexusConnectionState.resolve(device: missing, connected: true) == .unconfigured)

        let invalidIdentityURL = root.appendingPathComponent("invalid-device.json")
        try Data("not-json".utf8).write(to: invalidIdentityURL)
        let invalid = NexusDeviceStatus.load(from: invalidIdentityURL)
        precondition(invalid.error != nil)
        precondition(NexusConnectionState.resolve(device: invalid, connected: true) == .configurationError)
    }

    private static func testDesktopUpdateCheckDecoding() throws {
        let current = try DesktopUpdateCheck.decode(
            #"{"update_available":false,"message":"当前已是最新版本：v0.7.2"}"#
        )
        precondition(!current.updateAvailable)
        precondition(current.message.contains("最新版本"))

        let available = try DesktopUpdateCheck.decode(
            #"{"update_available":true,"message":"发现 AgentDock App 更新"}"#
        )
        precondition(available.updateAvailable)

        expectFailure("解析") {
            _ = try DesktopUpdateCheck.decode("not-json")
        }
    }

    private static func expectFailure(_ expected: String, operation: () throws -> Void) {
        do {
            try operation()
            preconditionFailure("expected validation failure containing: \(expected)")
        } catch {
            precondition(error.localizedDescription.contains(expected), error.localizedDescription)
        }
    }
}
