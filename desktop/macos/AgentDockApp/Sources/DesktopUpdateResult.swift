import Foundation

struct DesktopUpdateResult: Decodable {
    let schemaVersion: Int
    let transactionID: String?
    let ok: Bool
    let currentVersion: String
    let targetVersion: String
    let message: String

    private enum CodingKeys: String, CodingKey {
        case schemaVersion = "schema_version"
        case transactionID = "transaction_id"
        case ok
        case currentVersion = "current_version"
        case targetVersion = "target_version"
        case message
    }

    static func load(from path: URL) -> DesktopUpdateResult? {
        guard let data = try? Data(contentsOf: path) else { return nil }
        guard let result = try? JSONDecoder().decode(DesktopUpdateResult.self, from: data),
              result.schemaVersion == 1 else {
            return nil
        }
        return result
    }

    static func consume(from path: URL) -> DesktopUpdateResult? {
        guard let result = load(from: path) else { return nil }
        try? FileManager.default.removeItem(at: path)
        return result
    }
}

struct DesktopUpdateTerminalFailure: Decodable {
    let code: String
    let message: String
}

struct DesktopUpdateTerminalResult: Decodable {
    static let schemaVersion = 1

    let schemaVersion: Int
    let transactionID: String
    let platform: String
    let sourceVersion: String
    let targetVersion: String
    let state: String
    let failure: DesktopUpdateTerminalFailure?
    let warnings: [String]?

    private enum CodingKeys: String, CodingKey {
        case schemaVersion = "schema_version"
        case transactionID = "transaction_id"
        case platform
        case sourceVersion = "source_version"
        case targetVersion = "target_version"
        case state
        case failure
        case warnings
    }

    var isTerminal: Bool {
        state == "committed" || state == "rolled_back" || state == "failed"
    }

    static func load(from path: URL, transactionID: String) -> DesktopUpdateTerminalResult? {
        guard let data = try? Data(contentsOf: path),
              let result = try? JSONDecoder().decode(DesktopUpdateTerminalResult.self, from: data),
              result.schemaVersion == Self.schemaVersion,
              result.platform == "darwin",
              result.transactionID == transactionID,
              result.isTerminal else {
            return nil
        }
        return result
    }
}
