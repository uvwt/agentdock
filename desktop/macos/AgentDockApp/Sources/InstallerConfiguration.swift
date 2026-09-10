import Foundation

enum TunnelMode: String, CaseIterable {
    case local = "none"
    case quick
    case named

    var title: String {
        switch self {
        case .local: return L10n.text("Local only")
        case .quick: return L10n.text("Temporary public access")
        case .named: return L10n.text("Use your own Cloudflare domain")
        }
    }

    var detail: String {
        switch self {
        case .local:
            return L10n.text("Only allow this Mac to access AgentDock. Cloudflare public access stays disabled; you can configure your own tunnel or reverse proxy.")
        case .quick:
            return L10n.text("Automatically generate a temporary public address through Cloudflare without configuring a domain. Suitable for temporary access or testing; the address may change.")
        case .named:
            return L10n.text("Use your own HTTPS domain through Cloudflare Tunnel. Once configured, the public address remains stable.")
        }
    }
}

struct InstallRequest {
    let mode: TunnelMode
    let serverURL: String
    let tunnelToken: String

    init(mode: TunnelMode, serverURL: String, tunnelToken: String) {
        self.mode = mode
        self.serverURL = serverURL
        self.tunnelToken = tunnelToken
    }

    func validatedServerURL() throws -> String? {
        guard mode == .named else { return nil }
        let candidate = serverURL.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !candidate.isEmpty else {
            throw ValidationError(L10n.text("Enter a fixed HTTPS public address."))
        }
        guard var components = URLComponents(string: candidate) else {
            throw ValidationError(L10n.text("Invalid public address format."))
        }
        guard components.scheme?.lowercased() == "https" else {
            throw ValidationError(L10n.text("The public address must use https://."))
        }
        guard let host = components.host?.lowercased(), !host.isEmpty else {
            throw ValidationError(L10n.text("The public address is missing a valid domain."))
        }
        guard host != "localhost", host.contains("."), !isIPAddress(host) else {
            throw ValidationError(L10n.text("The public address must use a domain, not localhost or an IP address."))
        }
        guard components.user == nil,
              components.password == nil,
              components.port == nil,
              components.query == nil,
              components.fragment == nil else {
            throw ValidationError(L10n.text("Enter only the HTTPS origin; do not include credentials, a port, query parameters, or a fragment."))
        }
        let path = components.percentEncodedPath
        guard path.isEmpty || path == "/" else {
            throw ValidationError(L10n.text("The public address cannot contain a path. Do not include /mcp."))
        }
        components.path = ""
        components.query = nil
        components.fragment = nil
        guard let normalized = components.string else {
            throw ValidationError(L10n.text("Unable to normalize the public address."))
        }
        return normalized.hasSuffix("/") ? String(normalized.dropLast()) : normalized
    }

    func validatedTunnelToken() throws -> String? {
        guard mode == .named else { return nil }
        let token = tunnelToken.trimmingCharacters(in: .whitespacesAndNewlines)
        if token.isEmpty {
            // InstallerRunner 会从当前用户私密存储中复用此前保存的 Token；
            // 新安装或没有存储值时再给出明确错误。
            return nil
        }
        guard !token.contains("\n"), !token.contains("\r") else {
            throw ValidationError(L10n.text("Tunnel Token must be a single line of text."))
        }
        return token
    }

    private func isIPAddress(_ host: String) -> Bool {
        let ipv4Parts = host.split(separator: ".", omittingEmptySubsequences: false)
        if ipv4Parts.count == 4 && ipv4Parts.allSatisfy({ part in
            guard let value = Int(part) else { return false }
            return value >= 0 && value <= 255
        }) {
            return true
        }
        return host.contains(":")
    }
}

struct ValidationError: LocalizedError {
    let message: String

    init(_ message: String) {
        self.message = message
    }

    var errorDescription: String? { message }
}
