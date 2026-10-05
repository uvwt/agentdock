import Foundation

enum AppVersion {
    static var current: String {
        // prerelease 的完整产品版本不能塞进 macOS 数值型 bundle version 字段。
        // Release 构建单独写入 AgentDockReleaseVersion，旧包仍回退到系统短版本字段。
        let raw = Bundle.main.object(forInfoDictionaryKey: "AgentDockReleaseVersion") as? String
            ?? Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String
        return display(raw)
    }

    static func matchesCoreVersion(_ output: String, expectedDisplayVersion: String = current) -> Bool {
        output.split(whereSeparator: \.isNewline).first.map(String.init)
            == "AgentDock \(expectedDisplayVersion)"
    }

    static func matchesHealthVersion(_ raw: String?, expectedDisplayVersion: String = current) -> Bool {
        display(raw) == expectedDisplayVersion
    }

    static func display(_ raw: String?) -> String {
        guard let raw else { return L10n.text("Unknown version") }
        let value = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !value.isEmpty else { return L10n.text("Unknown version") }
        return value.hasPrefix("v") ? value : "v\(value)"
    }
}
