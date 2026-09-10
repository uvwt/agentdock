import Darwin
import Foundation

struct DesktopUpdateHandoff: Codable {
    static let schemaVersion = 1

    let schemaVersion: Int
    let targetVersion: String

    private enum CodingKeys: String, CodingKey {
        case schemaVersion = "schema_version"
        case targetVersion = "target_version"
    }

    init(targetVersion: String) {
        schemaVersion = Self.schemaVersion
        self.targetVersion = targetVersion
    }

    func write(to path: URL) throws {
        let fileManager = FileManager.default
        let directory = path.deletingLastPathComponent()
        try fileManager.createDirectory(at: directory, withIntermediateDirectories: true)
        try fileManager.setAttributes([.posixPermissions: 0o700], ofItemAtPath: directory.path)

        var data = try JSONEncoder().encode(self)
        data.append(0x0a)
        let temporary = directory.appendingPathComponent(".\(path.lastPathComponent).tmp.\(UUID().uuidString)")
        defer { try? fileManager.removeItem(at: temporary) }
        guard fileManager.createFile(atPath: temporary.path, contents: data, attributes: [.posixPermissions: 0o600]) else {
            throw ValidationError(L10n.text("Unable to create AgentDock update handoff confirmation."))
        }
        try fileManager.setAttributes([.posixPermissions: 0o600], ofItemAtPath: temporary.path)
        if Darwin.rename(temporary.path, path.path) != 0 {
            throw ValidationError(L10n.format(
                "Unable to save AgentDock update handoff confirmation: %@",
                String(cString: strerror(errno))
            ))
        }
    }

    static func remove(at path: URL) {
        try? FileManager.default.removeItem(at: path)
    }
}
