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
    let acpProfiles: [ACPProfileConfiguration]
    let acpDefaultProfile: String

    init(
        port: Int,
        logLevel: String,
        mcpAppsEnabled: Bool,
        browserEnabled: Bool,
        browserCDPURL: String,
        browserReuseExistingCDP: Bool,
        acpEnabled: Bool,
        acpProfiles: [ACPProfileConfiguration] = [],
        acpDefaultProfile: String = ""
    ) {
        self.port = port
        self.logLevel = logLevel
        self.mcpAppsEnabled = mcpAppsEnabled
        self.browserEnabled = browserEnabled
        self.browserCDPURL = browserCDPURL
        self.browserReuseExistingCDP = browserReuseExistingCDP
        self.acpEnabled = acpEnabled
        self.acpProfiles = acpProfiles
        self.acpDefaultProfile = acpDefaultProfile
    }

    func validated() throws -> EditableServiceSettings {
        try ServicePortValidation.validate(port)
        let normalizedLogLevel = ServiceConfiguration.normalizedLogLevel(logLevel)
        guard ["debug", "info", "warn", "error"].contains(normalizedLogLevel) else {
            throw ValidationError(L10n.text("Log level must be debug, info, warn, or error."))
        }

        let browserCDPURL = try Self.normalizeBrowserCDPURL(browserCDPURL)
        if browserEnabled, browserCDPURL.isEmpty, !browserReuseExistingCDP, BrowserSupportController.detectExecutable() == nil {
            throw ValidationError(L10n.text("No supported Chrome, Chromium, or Microsoft Edge was detected and no external CDP is configured."))
        }

        return try validatedWithProfiles(normalizedLogLevel: normalizedLogLevel, browserCDPURL: browserCDPURL)
    }

    private func validatedWithProfiles(normalizedLogLevel: String, browserCDPURL: String) throws -> EditableServiceSettings {
        var seen = Set<String>()
        var profiles: [ACPProfileConfiguration] = []
        profiles.reserveCapacity(acpProfiles.count)

        for rawProfile in acpProfiles {
            var profile = rawProfile
            profile.id = profile.id.trimmingCharacters(in: .whitespacesAndNewlines)
            guard Self.validProfileID(profile.id) else {
                throw ValidationError(L10n.format("Invalid Coding Agent profile ID: %@", profile.id))
            }
            guard seen.insert(profile.id).inserted else {
                throw ValidationError(L10n.format("Duplicate Coding Agent profile ID: %@", profile.id))
            }
            switch profile.kind {
            case .codex, .claude, .grok:
                guard profile.id == profile.kind.rawValue else {
                    throw ValidationError(L10n.format("Built-in Coding Agent %@ must use profile ID %@.", profile.kind.title, profile.kind.rawValue))
                }
            case .custom:
                guard !["codex", "claude", "grok"].contains(profile.id) else {
                    throw ValidationError(L10n.format("Custom Coding Agent profile ID %@ is reserved.", profile.id))
                }
            }

            if acpEnabled, profile.enabled {
                let resolution = profile.kind.resolveAdapter(
                    configuredCommand: profile.command,
                    configuredArguments: profile.args
                )
                guard resolution.available else {
                    throw ValidationError(L10n.format("%@ is unavailable: %@.", profile.id, profile.kind.missingAdapterMessage))
                }
                profile.command = resolution.command
                profile.args = resolution.arguments
            }
            profiles.append(profile)
        }

        let enabledProfiles = profiles.filter(\.enabled)
        if acpEnabled, enabledProfiles.isEmpty {
            throw ValidationError(L10n.text("Enable at least one Coding Agent profile."))
        }
        let defaultProfileID = acpDefaultProfile.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
            ? enabledProfiles.first?.id ?? profiles.first?.id ?? ""
            : acpDefaultProfile.trimmingCharacters(in: .whitespacesAndNewlines)
        guard let defaultProfile = profiles.first(where: { $0.id == defaultProfileID }),
              !acpEnabled || defaultProfile.enabled else {
            throw ValidationError(L10n.text("The default Coding Agent profile must reference an enabled profile."))
        }

        return EditableServiceSettings(
            port: port,
            logLevel: normalizedLogLevel,
            mcpAppsEnabled: mcpAppsEnabled,
            browserEnabled: browserEnabled,
            browserCDPURL: browserCDPURL,
            browserReuseExistingCDP: browserReuseExistingCDP,
            acpEnabled: acpEnabled,
            acpProfiles: profiles,
            acpDefaultProfile: defaultProfileID
        )
    }

    private static func validProfileID(_ value: String) -> Bool {
        guard !value.isEmpty, value.utf8.count <= 64 else { return false }
        return value.unicodeScalars.allSatisfy { scalar in
            let value = scalar.value
            return (65...90).contains(value)
                || (97...122).contains(value)
                || (48...57).contains(value)
                || value == 46
                || value == 95
                || value == 45
        }
    }

    private static func normalizeBrowserCDPURL(_ raw: String) throws -> String {
        let candidate = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !candidate.isEmpty else { return "" }
        guard let components = URLComponents(string: candidate),
              let scheme = components.scheme?.lowercased(),
              ["http", "https", "ws", "wss"].contains(scheme),
              components.host?.isEmpty == false else {
            throw ValidationError(L10n.text("Browser CDP address must be a valid HTTP(S) or WS(S) address."))
        }
        guard components.user == nil,
              components.password == nil,
              components.fragment == nil else {
            throw ValidationError(L10n.text("Browser CDP address cannot contain credentials or a fragment."))
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
        let replacements = [
            "AGENTDOCK_PORT": String(settings.port),
            "AGENTDOCK_LOG_LEVEL": settings.logLevel,
            "AGENTDOCK_MCP_APPS_ENABLED": settings.mcpAppsEnabled ? "true" : "false",
            "AGENTDOCK_BROWSER_ENABLED": settings.browserEnabled ? "true" : "false",
            "AGENTDOCK_BROWSER_CDP_URL": settings.browserCDPURL,
            "AGENTDOCK_BROWSER_REUSE_EXISTING_CDP": settings.browserReuseExistingCDP ? "true" : "false",
            "AGENTDOCK_ACP_ENABLED": settings.acpEnabled ? "true" : "false",
            "AGENTDOCK_ACP_PROFILES_JSON": try ACPDesktopConfiguration.encodeProfiles(settings.acpProfiles),
            "AGENTDOCK_ACP_DEFAULT_PROFILE": settings.acpDefaultProfile,
        ]
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
                throw ValidationError(L10n.format(
                    "Failed to start the new configuration, and validation after restoring the old configuration also failed: %@",
                    error.localizedDescription
                ))
            }
            throw ValidationError(L10n.format(
                "Failed to start the new configuration; restored the previous configuration: %@",
                originalError.localizedDescription
            ))
        }
    }

    private func readPrivateRegularFile(_ url: URL) throws -> Data {
        let values = try url.resourceValues(forKeys: [.isRegularFileKey, .isSymbolicLinkKey])
        guard values.isRegularFile == true, values.isSymbolicLink != true else {
            throw ValidationError(L10n.text("AgentDock configuration must be a regular file, not a symbolic link."))
        }
        let attributes = try fileManager.attributesOfItem(atPath: url.path)
        if let owner = attributes[.ownerAccountID] as? NSNumber,
           owner.uint32Value != getuid() {
            throw ValidationError(L10n.text("AgentDock configuration file does not belong to the current user."))
        }
        if let permissions = attributes[.posixPermissions] as? NSNumber,
           permissions.intValue & 0o077 != 0 {
            throw ValidationError(L10n.text("AgentDock configuration file permissions are too broad. Restore them to 0600 first."))
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
            throw ValidationError(L10n.text("Unable to create the temporary AgentDock configuration file."))
        }
        try fileManager.setAttributes([.posixPermissions: 0o600], ofItemAtPath: temporary.path)
        if Darwin.rename(temporary.path, url.path) != 0 {
            throw ValidationError(L10n.format(
                "Unable to atomically replace AgentDock configuration: %@",
                String(cString: strerror(errno))
            ))
        }
    }
}
