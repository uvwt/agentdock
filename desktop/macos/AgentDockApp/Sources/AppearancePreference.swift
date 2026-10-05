import AppKit
import Foundation

enum UIThemePreference: String, CaseIterable {
    case system
    case light
    case dark

    var title: String {
        switch self {
        case .system:
            return L10n.text("Follow system")
        case .light:
            return L10n.text("Light")
        case .dark:
            return L10n.text("Dark")
        }
    }
}

@MainActor
enum AppAppearance {
    private static let themePreferenceKey = "AgentDockUITheme"

    static func preference(defaults: UserDefaults = .standard) -> UIThemePreference {
        guard let rawValue = defaults.string(forKey: themePreferenceKey),
              let preference = UIThemePreference(rawValue: rawValue) else {
            return .system
        }
        return preference
    }

    static func setPreference(_ preference: UIThemePreference, defaults: UserDefaults = .standard) {
        if preference == .system {
            defaults.removeObject(forKey: themePreferenceKey)
        } else {
            defaults.set(preference.rawValue, forKey: themePreferenceKey)
        }
        apply(preference)
    }

    static func applyStoredPreference(defaults: UserDefaults = .standard) {
        apply(preference(defaults: defaults))
    }

    private static func apply(_ preference: UIThemePreference) {
        switch preference {
        case .system:
            NSApp.appearance = nil
        case .light:
            NSApp.appearance = NSAppearance(named: .aqua)
        case .dark:
            NSApp.appearance = NSAppearance(named: .darkAqua)
        }
    }
}
