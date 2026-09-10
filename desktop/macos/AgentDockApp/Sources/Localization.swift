import Foundation

enum UILanguagePreference: String, CaseIterable {
    case system
    case simplifiedChinese = "zh-Hans"
    case english = "en"

    var title: String {
        switch self {
        case .system:
            return L10n.text("Follow system")
        case .simplifiedChinese:
            return L10n.text("Simplified Chinese")
        case .english:
            return L10n.text("English")
        }
    }
}

enum L10n {
    private static let languagePreferenceKey = "AgentDockUILanguage"

    static func languagePreference(defaults: UserDefaults = .standard) -> UILanguagePreference {
        guard let rawValue = defaults.string(forKey: languagePreferenceKey),
              let preference = UILanguagePreference(rawValue: rawValue) else {
            return .system
        }
        return preference
    }

    static func setLanguagePreference(_ preference: UILanguagePreference, defaults: UserDefaults = .standard) {
        if preference == .system {
            defaults.removeObject(forKey: languagePreferenceKey)
        } else {
            defaults.set(preference.rawValue, forKey: languagePreferenceKey)
        }
    }

    static func text(_ key: String) -> String {
        NSLocalizedString(
            key,
            tableName: nil,
            bundle: localizationBundle(for: languagePreference()),
            value: key,
            comment: ""
        )
    }

    static func format(_ key: String, _ arguments: CVarArg...) -> String {
        String(format: text(key), locale: Locale.current, arguments: arguments)
    }

    private static func localizationBundle(for preference: UILanguagePreference) -> Bundle {
        guard preference != .system,
              let path = Bundle.main.path(forResource: preference.rawValue, ofType: "lproj"),
              let bundle = Bundle(path: path) else {
            return .main
        }
        return bundle
    }
}
