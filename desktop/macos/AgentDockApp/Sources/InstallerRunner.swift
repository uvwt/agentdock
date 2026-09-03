import Darwin
import Foundation

struct InstallResult: Decodable {
    let schemaVersion: Int
    let ok: Bool
    let version: String
    let healthy: Bool
    let localMCPURL: String
    let tunnelMode: String
    let publicURL: String
    let publicMCPURL: String
    let authToken: String
    let oauthPassword: String

    enum CodingKeys: String, CodingKey {
        case schemaVersion = "schema_version"
        case ok
        case version
        case healthy
        case localMCPURL = "local_mcp_url"
        case tunnelMode = "tunnel_mode"
        case publicURL = "public_url"
        case publicMCPURL = "public_mcp_url"
        case authToken = "auth_token"
        case oauthPassword = "oauth_password"
    }
}

enum QuickTunnelBootstrap {
    static func prepareInitialCoreEnvironment(_ values: inout [String: String]) {
        values.removeValue(forKey: "AGENTDOCK_SERVER_URL")
        values["AGENTDOCK_OAUTH_ENABLED"] = "false"
    }
}

final class InstallerRunner {
    private let service: ServiceController
    private let paths: AppPaths
    private let fileManager = FileManager.default

    init(service: ServiceController) {
        self.service = service
        paths = service.paths
    }

    func run(request: InstallRequest) async throws -> InstallResult {
        let serverURL = try request.validatedServerURL()
        let providedTunnelToken = try request.validatedTunnelToken()
        try validateBundledRuntime()
        try service.validatePersistentAppLocation()

        // 旧桌面版把 Named Tunnel Token 放在 cloudflared.env。先迁入独立 token store，
        // 后面的运行时清理只删除程序入口，不触碰用户凭据。
        try TunnelTokenStore(paths: paths).captureExistingTokenIfPresent()

        let previousStatus = await service.status()
        let previousCoreEnabled = previousStatus.autostartEnabled || previousStatus.requiresApproval
        let previousTunnelEnabled = service.tunnelEnabled()
        let snapshots = try snapshotManagedFiles()
        var legacyMigration: LegacyDesktopRuntimeMigration.Transaction?

        do {
            // 配置切换前先停掉受 SMAppService 管理的进程，避免旧 Tunnel 或 Core
            // 在事务中途读取到一半新、一半旧的配置。
            try service.setTunnelEnabled(false)
            if previousCoreEnabled {
                try await service.stop()
            }

            legacyMigration = try LegacyDesktopRuntimeMigration(paths: paths).begin()

            let prepared = try prepareConfiguration(
                request: request,
                serverURL: serverURL,
                providedTunnelToken: providedTunnelToken
            )
            try await service.runInBackground {
                try self.writePreparedConfiguration(prepared)
                try self.bootstrapCoreSkills()
            }

            try await service.start()
            if request.mode != .local {
                try service.setTunnelEnabled(true)
                if !(await service.waitForTunnelProcess()) {
                    // App Bundle 被原子替换后，SMAppService 可能仍显示已注册，
                    // 但实际的 Tunnel job 没有随之启动。对 Quick Tunnel 也
                    // 需要与 Named Tunnel 相同的自愈，否则会一直等到 URL 超时。
                    try service.restartTunnel()
                    guard await service.waitForTunnelProcess() else {
                        throw ValidationError("AgentDock Tunnel 已重新注册，但 cloudflared 没有稳定运行。")
                    }
                }
            }

            let publicURL: String
            switch request.mode {
            case .local:
                publicURL = ""
            case .named:
                publicURL = serverURL ?? ""
            case .quick:
                publicURL = try await waitForQuickTunnelURL(timeout: 35)
                guard let configuration = ServiceConfiguration.load(from: paths.environment),
                      await service.waitForHealth(configuration: configuration) else {
                    throw ValidationError("临时公网地址已生成，但 AgentDock Core 没有恢复健康。")
                }
            }

            let finalConfiguration = ServiceConfiguration.load(from: paths.environment)
            guard let finalConfiguration,
                  let localMCPURL = finalConfiguration.localMCPURL?.absoluteString else {
                throw ValidationError("AgentDock 配置已写入，但无法读取最终本地 MCP 地址。")
            }
            let publicMCPURL = publicURL.isEmpty ? "" : publicURL.trimmingCharacters(in: CharacterSet(charactersIn: "/")) + "/mcp"
            try legacyMigration?.commit()
            return InstallResult(
                schemaVersion: 1,
                ok: true,
                version: AppVersion.current,
                healthy: true,
                localMCPURL: localMCPURL,
                tunnelMode: request.mode.rawValue,
                publicURL: publicURL,
                publicMCPURL: publicMCPURL,
                authToken: prepared.authToken,
                oauthPassword: prepared.oauthPassword
            )
        } catch {
            let originalError = error
            do {
                try service.setTunnelEnabled(false)
                try await service.stop()
                try restoreManagedFiles(snapshots)
                try legacyMigration?.rollback()
                if previousCoreEnabled {
                    try await service.start()
                }
                if previousTunnelEnabled {
                    try service.setTunnelEnabled(true)
                }
            } catch {
                throw ValidationError("应用配置失败，而且安装前状态恢复也失败：\(originalError.localizedDescription)；\(error.localizedDescription)")
            }
            throw originalError
        }
    }

    private struct PreparedConfiguration {
        let environment: Data
        let tunnelEnvironment: Data
        let tunnelToken: String?
        let authToken: String
        let oauthPassword: String
    }

    private func prepareConfiguration(
        request: InstallRequest,
        serverURL: String?,
        providedTunnelToken: String?
    ) throws -> PreparedConfiguration {
        var values: [String: String] = [:]
        if fileManager.fileExists(atPath: paths.environment.path) {
            values = try ManagedEnvironment.load(from: paths.environment).values
        }

        values["AGENTDOCK_HOST"] = values["AGENTDOCK_HOST"]?.isEmpty == false ? values["AGENTDOCK_HOST"] : "127.0.0.1"
        let port = validPort(values["AGENTDOCK_PORT"]) ?? 8765
        values["AGENTDOCK_PORT"] = String(port)
        let logLevel = ServiceConfiguration.normalizedLogLevel(values["AGENTDOCK_LOG_LEVEL"] ?? "info")
        values["AGENTDOCK_LOG_LEVEL"] = ["debug", "info", "warn", "error"].contains(logLevel) ? logLevel : "info"

        let authToken = nonEmpty(values["AGENTDOCK_AUTH_TOKEN"]) ?? randomHex(byteCount: 32)
        let oauthPassword = nonEmpty(values["AGENTDOCK_OAUTH_PASSWORD"]) ?? randomHex(byteCount: 12)
        let oauthSecret = nonEmpty(values["AGENTDOCK_OAUTH_TOKEN_SECRET"]) ?? randomHex(byteCount: 32)
        values["AGENTDOCK_AUTH_TOKEN"] = authToken
        values["AGENTDOCK_OAUTH_PASSWORD"] = oauthPassword
        values["AGENTDOCK_OAUTH_TOKEN_SECRET"] = oauthSecret

        // 旧裸机部署使用的本地 codesign 参数不属于运行配置；App Bundle 统一签名后不再继承。
        for key in [
            "AGENTDOCK_CODESIGN_IDENTITY",
            "AGENTDOCK_CODESIGN_KEYCHAIN",
            "AGENTDOCK_CODESIGN_KEYCHAIN_PASSWORD",
            "AGENTDOCK_CODESIGN_IDENTIFIER",
            "AGENTDOCK_CODESIGN_HOME",
        ] {
            values.removeValue(forKey: key)
        }

        var tunnelValues = ["AGENTDOCK_TUNNEL_MODE": request.mode.rawValue]
        var tunnelToken: String?
        switch request.mode {
        case .local:
            values.removeValue(forKey: "AGENTDOCK_SERVER_URL")
            values["AGENTDOCK_OAUTH_ENABLED"] = "false"
        case .quick:
            QuickTunnelBootstrap.prepareInitialCoreEnvironment(&values)
            tunnelValues["AGENTDOCK_TUNNEL_TARGET"] = "http://127.0.0.1:\(port)"
        case .named:
            guard let serverURL else {
                throw ValidationError("固定域名模式缺少 HTTPS 公网地址。")
            }
            let tokenStore = TunnelTokenStore(paths: paths)
            tunnelToken = try tokenStore.tokenForNamedTunnel(providedToken: providedTunnelToken)
            values["AGENTDOCK_SERVER_URL"] = serverURL
            values["AGENTDOCK_OAUTH_ENABLED"] = "true"
        }

        return PreparedConfiguration(
            environment: renderEnvironment(values),
            tunnelEnvironment: renderEnvironment(tunnelValues),
            tunnelToken: tunnelToken,
            authToken: authToken,
            oauthPassword: oauthPassword
        )
    }

    private func writePreparedConfiguration(_ prepared: PreparedConfiguration) throws {
        try createRuntimeDirectories()
        try writePrivateAtomically(prepared.environment, to: paths.environment)
        try writePrivateAtomically(prepared.tunnelEnvironment, to: paths.tunnelEnvironment)
        try? fileManager.removeItem(at: paths.quickTunnelURL)
        if let token = prepared.tunnelToken {
            try TunnelTokenStore(paths: paths).persist(token)
        }
    }

    private func validateBundledRuntime() throws {
        for (url, title) in [
            (paths.binary, "AgentDock Core"),
            (paths.cloudflared, "cloudflared"),
        ] {
            let values = try url.resourceValues(forKeys: [.isRegularFileKey, .isSymbolicLinkKey])
            guard values.isRegularFile == true,
                  values.isSymbolicLink != true,
                  fileManager.isExecutableFile(atPath: url.path) else {
                throw ValidationError("AgentDock.app 缺少有效的 \(title)：\(url.path)")
            }
        }
        let manifest = paths.coreSkillBundle.appendingPathComponent("manifest.json")
        let manifestValues = try manifest.resourceValues(forKeys: [.isRegularFileKey, .isSymbolicLinkKey])
        guard manifestValues.isRegularFile == true, manifestValues.isSymbolicLink != true else {
            throw ValidationError("AgentDock.app 缺少官方核心 Skill Bundle。")
        }

        let version = try runProcess(executable: paths.binary.path, arguments: ["--version"])
        guard version.status == 0,
              AppVersion.matchesCoreVersion(version.output) else {
            throw ValidationError("AgentDock.app 内置 Core 与 App 版本不一致，请重新安装应用。")
        }
        let cloudflared = try runProcess(executable: paths.cloudflared.path, arguments: ["--version"])
        guard cloudflared.status == 0 else {
            throw ValidationError("AgentDock.app 内的 cloudflared 无法运行。")
        }
    }

    private func bootstrapCoreSkills() throws {
        let result = try runProcess(
            executable: paths.binary.path,
            arguments: ["skill", "bootstrap", "--bundle", paths.coreSkillBundle.path]
        )
        guard result.status == 0 else {
            throw ValidationError(result.output.isEmpty ? "官方核心 Skill 初始化失败。" : result.output)
        }
    }

    private func createRuntimeDirectories() throws {
        for (url, permissions) in [
            (paths.appSupport, 0o700),
            (paths.stateDirectory, 0o700),
            (paths.workDirectory, 0o700),
            (paths.logs, 0o700),
        ] {
            try fileManager.createDirectory(at: url, withIntermediateDirectories: true)
            try fileManager.setAttributes(
                [.posixPermissions: NSNumber(value: Int16(permissions))],
                ofItemAtPath: url.path
            )
        }
    }

    private struct FileSnapshot {
        let url: URL
        let data: Data?
    }

    private func snapshotManagedFiles() throws -> [FileSnapshot] {
        try [paths.environment, paths.tunnelEnvironment, paths.tunnelTokenStore, paths.quickTunnelURL].map { url in
            guard fileManager.fileExists(atPath: url.path) else {
                return FileSnapshot(url: url, data: nil)
            }
            let values = try url.resourceValues(forKeys: [.isRegularFileKey, .isSymbolicLinkKey])
            guard values.isRegularFile == true, values.isSymbolicLink != true else {
                throw ValidationError("运行配置必须是普通文件：\(url.path)")
            }
            return FileSnapshot(url: url, data: try Data(contentsOf: url))
        }
    }

    private func restoreManagedFiles(_ snapshots: [FileSnapshot]) throws {
        for snapshot in snapshots {
            if let data = snapshot.data {
                try writePrivateAtomically(data, to: snapshot.url)
            } else {
                try? fileManager.removeItem(at: snapshot.url)
            }
        }
    }

    private func writePrivateAtomically(_ data: Data, to url: URL) throws {
        let directory = url.deletingLastPathComponent()
        try fileManager.createDirectory(at: directory, withIntermediateDirectories: true)
        let temporary = directory.appendingPathComponent(".\(url.lastPathComponent).tmp.\(UUID().uuidString)")
        defer { try? fileManager.removeItem(at: temporary) }
        guard fileManager.createFile(atPath: temporary.path, contents: data, attributes: [.posixPermissions: 0o600]) else {
            throw ValidationError("无法创建配置临时文件：\(url.lastPathComponent)")
        }
        try fileManager.setAttributes([.posixPermissions: 0o600], ofItemAtPath: temporary.path)
        if Darwin.rename(temporary.path, url.path) != 0 {
            throw ValidationError("无法原子替换 \(url.lastPathComponent)：\(String(cString: strerror(errno)))")
        }
    }

    private func renderEnvironment(_ values: [String: String]) -> Data {
        let text = values.keys.sorted().map { key in
            "\(key)=\(ManagedEnvironment.shellQuote(values[key] ?? ""))"
        }.joined(separator: "\n") + "\n"
        return Data(text.utf8)
    }

    private func waitForQuickTunnelURL(timeout: TimeInterval) async throws -> String {
        try await service.runInBackground {
            let deadline = Date().addingTimeInterval(timeout)
            while Date() < deadline {
                if let data = try? Data(contentsOf: self.paths.quickTunnelURL),
                   let value = String(data: data, encoding: .utf8)?.trimmingCharacters(in: .whitespacesAndNewlines),
                   let url = URL(string: value),
                   url.scheme == "https",
                   url.host?.hasSuffix(".trycloudflare.com") == true {
                    return value
                }
                Thread.sleep(forTimeInterval: 0.25)
            }
            throw ValidationError("cloudflared 未在超时前生成临时公网地址。")
        }
    }

    private func validPort(_ value: String?) -> Int? {
        guard let value, let port = Int(value), (1...65535).contains(port) else { return nil }
        return port
    }

    private func nonEmpty(_ value: String?) -> String? {
        guard let value = value?.trimmingCharacters(in: .whitespacesAndNewlines), !value.isEmpty else { return nil }
        return value
    }

    private func randomHex(byteCount: Int) -> String {
        var generator = SystemRandomNumberGenerator()
        return (0..<byteCount).map { _ in
            String(format: "%02x", UInt8.random(in: .min ... .max, using: &generator))
        }.joined()
    }
}

struct ProcessExecution {
    let status: Int32
    let output: String
}

func runProcess(
    executable: String,
    arguments: [String],
    environment: [String: String] = [:]
) throws -> ProcessExecution {
    let process = Process()
    process.executableURL = URL(fileURLWithPath: executable)
    process.arguments = arguments
    if !environment.isEmpty {
        process.environment = ProcessInfo.processInfo.environment.merging(environment) { _, replacement in replacement }
    }
    let pipe = Pipe()
    process.standardOutput = pipe
    process.standardError = pipe
    try process.run()
    let outputData = pipe.fileHandleForReading.readDataToEndOfFile()
    process.waitUntilExit()
    return ProcessExecution(
        status: process.terminationStatus,
        output: String(data: outputData, encoding: .utf8) ?? ""
    )
}

func runUpdateProcess(
    executable: String,
    arguments: [String],
    environment: [String: String],
    outputURL: URL
) throws -> ProcessExecution {
    let outputDirectory = outputURL.deletingLastPathComponent()
    try FileManager.default.createDirectory(at: outputDirectory, withIntermediateDirectories: true)
    try FileManager.default.setAttributes(
        [.posixPermissions: 0o700],
        ofItemAtPath: outputDirectory.path
    )
    guard FileManager.default.createFile(
        atPath: outputURL.path,
        contents: nil,
        attributes: [.posixPermissions: 0o600]
    ) else {
        throw ValidationError("无法创建更新日志文件。")
    }
    try FileManager.default.setAttributes(
        [.posixPermissions: 0o600],
        ofItemAtPath: outputURL.path
    )
    let outputHandle = try FileHandle(forWritingTo: outputURL)
    defer { try? outputHandle.close() }

    let process = Process()
    process.executableURL = URL(fileURLWithPath: executable)
    process.arguments = arguments
    process.environment = ProcessInfo.processInfo.environment.merging(environment) { _, replacement in replacement }
    process.standardOutput = outputHandle
    process.standardError = outputHandle
    try process.run()
    process.waitUntilExit()
    try outputHandle.synchronize()

    let outputData = (try? Data(contentsOf: outputURL)) ?? Data()
    return ProcessExecution(
        status: process.terminationStatus,
        output: String(data: outputData, encoding: .utf8) ?? ""
    )
}
