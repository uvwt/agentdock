import Darwin
import Foundation

struct EditableServiceSettings {
    let port: Int
    let logLevel: String
    let mcpAppsEnabled: Bool
    let browserEnabled: Bool
    let browserCDPURL: String
    let browserReuseExistingCDP: Bool
    let acpEnabled: Bool
    let acpAgent: ACPAgentPreset
    let acpCommand: String
    let acpArgs: [String]

    func validated() throws -> EditableServiceSettings {
        try ServicePortValidation.validate(port)
        let normalizedLogLevel = ServiceConfiguration.normalizedLogLevel(logLevel)
        guard ["debug", "info", "warn", "error"].contains(normalizedLogLevel) else {
            throw ValidationError("日志级别必须是 debug、info、warn 或 error。")
        }

        let browserCDPURL = try Self.normalizeBrowserCDPURL(browserCDPURL)
        if browserEnabled, browserCDPURL.isEmpty, !browserReuseExistingCDP, BrowserSupportController.detectExecutable() == nil {
            throw ValidationError("未检测到受支持的 Chrome、Chromium 或 Microsoft Edge，且未配置外部 CDP。")
        }

        var command = acpAgent == .custom
            ? acpCommand.trimmingCharacters(in: .whitespacesAndNewlines)
            : ""
        var arguments = acpAgent == .custom ? acpArgs : []
        if acpEnabled {
            let resolution = acpAgent.resolveAdapter(
                configuredCommand: acpCommand,
                configuredArguments: acpArgs
            )
            guard resolution.available else {
                throw ValidationError("\(acpAgent.title) 不可用：\(acpAgent.missingAdapterMessage)。")
            }
            command = resolution.command
            arguments = resolution.arguments
        }

        return EditableServiceSettings(
            port: port,
            logLevel: normalizedLogLevel,
            mcpAppsEnabled: mcpAppsEnabled,
            browserEnabled: browserEnabled,
            browserCDPURL: browserCDPURL,
            browserReuseExistingCDP: browserReuseExistingCDP,
            acpEnabled: acpEnabled,
            acpAgent: acpAgent,
            acpCommand: command,
            acpArgs: arguments
        )
    }

    private static func normalizeBrowserCDPURL(_ raw: String) throws -> String {
        let candidate = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !candidate.isEmpty else { return "" }
        guard let components = URLComponents(string: candidate),
              let scheme = components.scheme?.lowercased(),
              ["http", "https", "ws", "wss"].contains(scheme),
              components.host?.isEmpty == false else {
            throw ValidationError("浏览器 CDP 地址必须是有效的 HTTP(S) 或 WS(S) 地址。")
        }
        guard components.user == nil,
              components.password == nil,
              components.fragment == nil else {
            throw ValidationError("浏览器 CDP 地址不能包含账号信息或片段。")
        }
        return candidate
    }
}

final class ServiceConfigurationController {
    private let service: ServiceController
    private let fileManager = FileManager.default

    init(service: ServiceController) {
        self.service = service
    }

    func apply(_ requested: EditableServiceSettings) async throws {
        let settings = try requested.validated()
        let environmentURL = service.paths.environment
        let originalData = try readPrivateRegularFile(environmentURL)
        let environment = try ManagedEnvironment.load(from: environmentURL)
        var replacements = [
            "AGENTDOCK_PORT": String(settings.port),
            "AGENTDOCK_LOG_LEVEL": settings.logLevel,
            "AGENTDOCK_MCP_APPS_ENABLED": settings.mcpAppsEnabled ? "true" : "false",
            "AGENTDOCK_BROWSER_ENABLED": settings.browserEnabled ? "true" : "false",
            "AGENTDOCK_BROWSER_CDP_URL": settings.browserCDPURL,
            "AGENTDOCK_BROWSER_REUSE_EXISTING_CDP": settings.browserReuseExistingCDP ? "true" : "false",
            "AGENTDOCK_ACP_ENABLED": settings.acpEnabled ? "true" : "false",
            "AGENTDOCK_ACP_AGENT": settings.acpAgent.rawValue,
            "AGENTDOCK_ACP_COMMAND": settings.acpCommand,
            "AGENTDOCK_ACP_ARGS_JSON": try ACPDesktopConfiguration.encodeArguments(settings.acpArgs),
        ]
        if settings.acpEnabled {
            // 桌面预设依赖各 Agent 自己的登录状态，不继承上一个 Agent 的密钥映射。
            replacements["AGENTDOCK_ACP_ENV_FROM_ENV_JSON"] = "{}"
        }
        let updatedData = try environment.dataByUpdating(replacements, removing: ServiceConfiguration.removableLegacyKeys)
        let wasLoaded = service.isLoaded()

        try await service.runInBackground {
            try self.writePrivateAtomically(updatedData, to: environmentURL)
        }
        guard wasLoaded else { return }

        do {
            try await service.restart()
        } catch {
            let originalError = error
            do {
                try await service.runInBackground {
                    try self.writePrivateAtomically(originalData, to: environmentURL)
                }
                try await service.restart()
            } catch {
                throw ValidationError("新配置启动失败，而且旧配置恢复验证也失败：\(error.localizedDescription)")
            }
            throw ValidationError("新配置启动失败，已恢复旧配置：\(originalError.localizedDescription)")
        }
    }

    private func readPrivateRegularFile(_ url: URL) throws -> Data {
        let values = try url.resourceValues(forKeys: [.isRegularFileKey, .isSymbolicLinkKey])
        guard values.isRegularFile == true, values.isSymbolicLink != true else {
            throw ValidationError("AgentDock 配置必须是普通文件，不能是符号链接。")
        }
        let attributes = try fileManager.attributesOfItem(atPath: url.path)
        if let owner = attributes[.ownerAccountID] as? NSNumber,
           owner.uint32Value != getuid() {
            throw ValidationError("AgentDock 配置文件不属于当前用户。")
        }
        if let permissions = attributes[.posixPermissions] as? NSNumber,
           permissions.intValue & 0o077 != 0 {
            throw ValidationError("AgentDock 配置文件权限过宽，请先恢复为 0600。")
        }
        return try Data(contentsOf: url)
    }

    private func writePrivateAtomically(_ data: Data, to url: URL) throws {
        let directory = url.deletingLastPathComponent()
        try fileManager.createDirectory(at: directory, withIntermediateDirectories: true)
        let temporary = directory.appendingPathComponent(".\(url.lastPathComponent).tmp.\(UUID().uuidString)")
        defer { try? fileManager.removeItem(at: temporary) }
        guard fileManager.createFile(
            atPath: temporary.path,
            contents: data,
            attributes: [.posixPermissions: 0o600]
        ) else {
            throw ValidationError("无法创建 AgentDock 配置临时文件。")
        }
        try fileManager.setAttributes([.posixPermissions: 0o600], ofItemAtPath: temporary.path)
        if Darwin.rename(temporary.path, url.path) != 0 {
            throw ValidationError("无法原子替换 AgentDock 配置：\(String(cString: strerror(errno)))")
        }
    }
}
