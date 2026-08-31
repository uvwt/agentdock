import Foundation

struct AppPaths {
    let home: URL
    let appBundle: URL

    init(
        home: URL = FileManager.default.homeDirectoryForCurrentUser,
        appBundle: URL = Bundle.main.bundleURL
    ) {
        self.home = home
        self.appBundle = appBundle
    }

    var binary: URL { appBundle.appendingPathComponent("Contents/Helpers/agentdock") }
    var cloudflared: URL { appBundle.appendingPathComponent("Contents/Helpers/cloudflared") }
    var coreSkillBundle: URL { appBundle.appendingPathComponent("Contents/Resources/core-skills") }
    var appSupport: URL { home.appendingPathComponent("Library/Application Support/AgentDock") }
    var environment: URL { appSupport.appendingPathComponent("agentdock.env") }
    var tunnelEnvironment: URL { appSupport.appendingPathComponent("cloudflared.env") }
    var tunnelTokenStore: URL { appSupport.appendingPathComponent("cloudflare-tunnel-token") }
    var quickTunnelURL: URL { appSupport.appendingPathComponent("quick-tunnel-url.txt") }
    var updateResult: URL { appSupport.appendingPathComponent("update-result.json") }
    var updateServiceState: URL { appSupport.appendingPathComponent("update-services.json") }
    var updateHandoff: URL { appSupport.appendingPathComponent("update-handoff.json") }
    var updateLog: URL { appSupport.appendingPathComponent("update.log") }
    var logs: URL { home.appendingPathComponent("Library/Logs/AgentDock") }
    var workDirectory: URL { home.appendingPathComponent("AgentDock") }
    var stateDirectory: URL { home.appendingPathComponent(".agentdock") }
    var nexusDeviceIdentity: URL {
        stateDirectory.appendingPathComponent("nexus").appendingPathComponent("device.json")
    }
}

struct ServiceConfiguration: Equatable {
    static let editableKeys = [
        "AGENTDOCK_PORT",
        "AGENTDOCK_LOG_LEVEL",
        "AGENTDOCK_BROWSER_ENABLED",
        "AGENTDOCK_BROWSER_CDP_URL",
        "AGENTDOCK_BROWSER_REUSE_EXISTING_CDP",
        "AGENTDOCK_ACP_ENABLED",
        "AGENTDOCK_ACP_AGENT",
        "AGENTDOCK_ACP_COMMAND",
        "AGENTDOCK_ACP_ARGS_JSON",
        "AGENTDOCK_ACP_ENV_FROM_ENV_JSON",
    ]
    // 旧 Nexus 凭据不再参与运行；保存设置时一并从环境文件清除，避免废弃密钥继续落盘。
    static let removableLegacyKeys: Set<String> = [
        "AGENTDOCK_ACP_ALLOWED_ROOTS",
        "AGENTDOCK_NEXUS_ENDPOINT",
        "AGENTDOCK_NEXUS_TOKEN",
    ]

    let host: String
    let port: Int
    let publicURL: String?
    let authToken: String
    let oauthPassword: String
    let logLevel: String
    let browserEnabled: Bool
    let browserCDPURL: String
    let browserReuseExistingCDP: Bool
    let acpEnabled: Bool
    let acpAgent: ACPAgentPreset
    let acpCommand: String
    let acpArgs: [String]

    var healthHost: String {
        switch host {
        case "0.0.0.0", "": return "127.0.0.1"
        case "::", "[::]": return "::1"
        default: return host
        }
    }

    var localMCPURL: URL? { endpoint(path: "/mcp") }
    var healthURL: URL? { endpoint(path: "/healthz") }

    var publicMCPURL: URL? {
        guard let publicURL, !publicURL.isEmpty else { return nil }
        return URL(string: publicURL.trimmingCharacters(in: CharacterSet(charactersIn: "/")) + "/mcp")
    }

    private func endpoint(path: String) -> URL? {
        var components = URLComponents()
        components.scheme = "http"
        components.host = healthHost
        components.port = port
        components.path = path
        return components.url
    }

    static func load(from path: URL) -> ServiceConfiguration? {
        guard let environment = try? ManagedEnvironment.load(from: path) else { return nil }
        let values = environment.values
        let host = values["AGENTDOCK_HOST"] ?? "127.0.0.1"
        guard let port = Int(values["AGENTDOCK_PORT"] ?? "8765"), (1...65535).contains(port) else { return nil }
        let publicURL = values["AGENTDOCK_SERVER_URL"].flatMap { $0.isEmpty ? nil : $0 }
        guard let acpAgent = ACPAgentPreset.parse(values["AGENTDOCK_ACP_AGENT"] ?? "codex") else {
            return nil
        }
        return ServiceConfiguration(
            host: host,
            port: port,
            publicURL: publicURL,
            authToken: values["AGENTDOCK_AUTH_TOKEN"] ?? "",
            oauthPassword: values["AGENTDOCK_OAUTH_PASSWORD"] ?? "",
            logLevel: normalizedLogLevel(values["AGENTDOCK_LOG_LEVEL"] ?? "info"),
            browserEnabled: parseBool(values["AGENTDOCK_BROWSER_ENABLED"]),
            browserCDPURL: values["AGENTDOCK_BROWSER_CDP_URL"] ?? "",
            browserReuseExistingCDP: parseBool(values["AGENTDOCK_BROWSER_REUSE_EXISTING_CDP"]),
            acpEnabled: parseBool(values["AGENTDOCK_ACP_ENABLED"]),
            acpAgent: acpAgent,
            acpCommand: values["AGENTDOCK_ACP_COMMAND"] ?? "",
            acpArgs: decodeStringArray(values["AGENTDOCK_ACP_ARGS_JSON"])
        )
    }

    static func normalizedLogLevel(_ raw: String) -> String {
        let value = raw.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        return value == "warning" ? "warn" : (value.isEmpty ? "info" : value)
    }

    private static func decodeStringArray(_ raw: String?) -> [String] {
        guard let raw, let data = raw.data(using: .utf8),
              let values = try? JSONDecoder().decode([String].self, from: data) else {
            return []
        }
        return values
    }

    private static func parseBool(_ raw: String?) -> Bool {
        switch raw?.trimmingCharacters(in: .whitespacesAndNewlines).lowercased() {
        case "1", "true", "yes", "on": return true
        default: return false
        }
    }
}

typealias RuntimeConfiguration = ServiceConfiguration

struct NexusDeviceStatus {
    let paired: Bool
    let endpoint: String
    let nodeID: String
    let deviceTokenStored: Bool
    let error: String?

    static func load(from path: URL) -> NexusDeviceStatus {
        guard FileManager.default.fileExists(atPath: path.path) else {
            return NexusDeviceStatus(paired: false, endpoint: "", nodeID: "", deviceTokenStored: false, error: nil)
        }
        do {
            let identity = try JSONDecoder().decode(NexusDeviceIdentity.self, from: Data(contentsOf: path))
            guard !identity.endpoint.isEmpty, !identity.nodeID.isEmpty,
                  !identity.deviceID.isEmpty, !identity.deviceToken.isEmpty else {
                throw ValidationError("设备身份文件无效，请重新配对。")
            }
            return NexusDeviceStatus(
                paired: true,
                endpoint: identity.endpoint,
                nodeID: identity.nodeID,
                deviceTokenStored: true,
                error: nil
            )
        } catch {
            return NexusDeviceStatus(
                paired: false,
                endpoint: "",
                nodeID: "",
                deviceTokenStored: false,
                error: "无法读取设备身份：\(error.localizedDescription)"
            )
        }
    }
}

private struct NexusDeviceIdentity: Decodable {
    let endpoint: String
    let nodeID: String
    let deviceID: String
    let deviceToken: String

    private enum CodingKeys: String, CodingKey {
        case endpoint
        case nodeID = "node_id"
        case deviceID = "device_id"
        case deviceToken = "device_token"
    }
}
