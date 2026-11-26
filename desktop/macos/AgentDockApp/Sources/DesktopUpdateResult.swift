import Foundation

struct DesktopUpdateResult: Decodable {
    let schemaVersion: Int
    let transactionID: String?
    let ok: Bool
    let currentVersion: String
    let targetVersion: String
    let previousAppPath: String?
    let message: String

    private enum CodingKeys: String, CodingKey {
        case schemaVersion = "schema_version"
        case transactionID = "transaction_id"
        case ok
        case currentVersion = "current_version"
        case targetVersion = "target_version"
        case previousAppPath = "previous_app_path"
        case message
    }

    func validatedPreviousAppBundle(currentApp: URL) -> URL? {
        guard let previousAppPath = previousAppPath?
            .trimmingCharacters(in: .whitespacesAndNewlines),
              !previousAppPath.isEmpty else {
            return nil
        }
        let previous = URL(fileURLWithPath: previousAppPath)
            .standardizedFileURL
            .resolvingSymlinksInPath()
        let current = currentApp.standardizedFileURL.resolvingSymlinksInPath()
        guard previous.deletingLastPathComponent().path == current.deletingLastPathComponent().path,
              previous.lastPathComponent.hasPrefix(".AgentDock.app.backup.") else {
            return nil
        }
        return previous
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
        guard consumeTriggerFile(at: path) else { return nil }
        return result
    }

    @discardableResult
    static func discard(from path: URL) -> Bool {
        consumeTriggerFile(at: path)
    }

    private static func consumeTriggerFile(at path: URL) -> Bool {
        let fileManager = FileManager.default
        guard fileManager.fileExists(atPath: path.path) else { return true }

        // 先把一次性启动触发器移出固定路径，再做 best-effort 删除。
        // 即使删除临时文件失败，已处理结果也不会在每次启动时重复触发 finishing UI。
        let consumed = path.deletingLastPathComponent().appendingPathComponent(
            ".\(path.lastPathComponent).consumed-\(UUID().uuidString)"
        )
        do {
            try fileManager.moveItem(at: path, to: consumed)
        } catch {
            NSLog("AgentDock could not consume update result trigger %@: %@", path.path, error.localizedDescription)
            return false
        }
        do {
            try fileManager.removeItem(at: consumed)
        } catch {
            NSLog("AgentDock could not delete consumed update result %@: %@", consumed.path, error.localizedDescription)
        }
        return true
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

    // Arbiter 写入 result.json 的系统级英文提示在这里统一做本地化映射与去重，
    // 兼顾旧版 Arbiter 升新版 App 的跨版本场景；未知提示原样保留，避免丢失诊断信息。
    static func localizedWarning(_ raw: String) -> String {
        let trimmed = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return "" }

        if trimmed == "AgentDock was updated from an ad-hoc signed macOS build. Accessibility, Screen Recording, or Automation permissions may require reauthorization because the App or Core code identity changed."
            || trimmed.hasPrefix("AgentDock was updated from an ad-hoc signed macOS build.") {
            return L10n.text("If Accessibility, Screen Recording, or Automation permissions stop working after this update, re-enable AgentDock in System Settings → Privacy & Security.")
        }
        if trimmed == "AgentDock Core requires background-item approval in System Settings."
            || trimmed == "AgentDock Core needs background-item approval in System Settings." {
            return L10n.text("AgentDock Core needs background-item approval in System Settings.")
        }
        if trimmed == "AgentDock Tunnel requires background-item approval in System Settings."
            || trimmed == "AgentDock Tunnel needs background-item approval in System Settings." {
            return L10n.text("AgentDock Tunnel needs background-item approval in System Settings.")
        }
        return trimmed
    }

    static func mergeWarnings(local: [String], terminal: [String]?) -> [String] {
        var merged: [String] = []
        for warning in local + (terminal ?? []) {
            let value = localizedWarning(warning)
            if !value.isEmpty, !merged.contains(value) {
                merged.append(value)
            }
        }
        return merged
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
