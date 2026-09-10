import Foundation

struct TunnelTokenStore {
    private static let maximumTokenBytes = 16 * 1024

    let paths: AppPaths

    init(paths: AppPaths = AppPaths()) {
        self.paths = paths
    }

    func captureExistingTokenIfPresent() throws {
        guard let token = try activeTunnelToken() else { return }
        try persist(token)
    }

    func tokenForNamedTunnel(providedToken: String?) throws -> String {
        if let providedToken {
            return try validated(providedToken)
        }
        if let stored = try storedToken() {
            return stored
        }
        if let active = try activeTunnelToken() {
            try persist(active)
            return active
        }
        throw ValidationError(L10n.text("Enter the Cloudflare Tunnel Token. Leave it blank to reuse a previously saved token."))
    }

    func persist(_ token: String) throws {
        let token = try validated(token)
        let fileManager = FileManager.default
        try fileManager.createDirectory(at: paths.appSupport, withIntermediateDirectories: true)
        try fileManager.setAttributes(
            [.posixPermissions: NSNumber(value: Int16(0o700))],
            ofItemAtPath: paths.appSupport.path
        )

        if fileManager.fileExists(atPath: paths.tunnelTokenStore.path) {
            let values = try paths.tunnelTokenStore.resourceValues(forKeys: [.isRegularFileKey, .isSymbolicLinkKey])
            guard values.isRegularFile == true, values.isSymbolicLink != true else {
                throw ValidationError(L10n.text("The saved Tunnel Token path is not a secure regular file."))
            }
        }

        guard let data = token.data(using: .utf8) else {
            throw ValidationError(L10n.text("Unable to encode the Cloudflare Tunnel Token."))
        }
        try data.write(to: paths.tunnelTokenStore, options: .atomic)
        try fileManager.setAttributes(
            [.posixPermissions: NSNumber(value: Int16(0o600))],
            ofItemAtPath: paths.tunnelTokenStore.path
        )
    }

    func storedToken() throws -> String? {
        let fileManager = FileManager.default
        guard fileManager.fileExists(atPath: paths.tunnelTokenStore.path) else { return nil }
        let values = try paths.tunnelTokenStore.resourceValues(forKeys: [.isRegularFileKey, .isSymbolicLinkKey])
        guard values.isRegularFile == true, values.isSymbolicLink != true else {
            throw ValidationError(L10n.text("The saved Tunnel Token path is not a secure regular file."))
        }
        let attributes = try fileManager.attributesOfItem(atPath: paths.tunnelTokenStore.path)
        let permissions = (attributes[.posixPermissions] as? NSNumber)?.intValue ?? 0o777
        guard permissions & 0o077 == 0 else {
            throw ValidationError(L10n.text("The saved Tunnel Token permissions are unsafe; only the current user should be able to read it."))
        }
        let data = try Data(contentsOf: paths.tunnelTokenStore)
        guard data.count <= Self.maximumTokenBytes,
              let value = String(data: data, encoding: .utf8) else {
            throw ValidationError(L10n.text("The saved Tunnel Token format is invalid."))
        }
        return try validated(value)
    }

    private func activeTunnelToken() throws -> String? {
        guard FileManager.default.fileExists(atPath: paths.tunnelEnvironment.path) else { return nil }
        let environment = try ManagedEnvironment.load(from: paths.tunnelEnvironment)
        guard let token = environment.values["TUNNEL_TOKEN"],
              !token.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else {
            return nil
        }
        return try validated(token)
    }

    private func validated(_ rawToken: String) throws -> String {
        let token = rawToken.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !token.isEmpty else {
            throw ValidationError(L10n.text("Cloudflare Tunnel Token cannot be empty."))
        }
        guard !token.contains("\n"), !token.contains("\r") else {
            throw ValidationError(L10n.text("Cloudflare Tunnel Token must be a single line of text."))
        }
        guard token.utf8.count <= Self.maximumTokenBytes else {
            throw ValidationError(L10n.text("Cloudflare Tunnel Token has an invalid length."))
        }
        return token
    }
}
