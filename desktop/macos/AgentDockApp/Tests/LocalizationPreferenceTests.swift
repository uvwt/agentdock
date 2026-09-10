import Foundation

@main
struct LocalizationPreferenceTests {
    static func main() {
        let suiteName = "AgentDock.LocalizationPreferenceTests.\(UUID().uuidString)"
        guard let defaults = UserDefaults(suiteName: suiteName) else {
            preconditionFailure("failed to create isolated UserDefaults")
        }
        defer { defaults.removePersistentDomain(forName: suiteName) }

        precondition(L10n.languagePreference(defaults: defaults) == .system)

        L10n.setLanguagePreference(.english, defaults: defaults)
        precondition(L10n.languagePreference(defaults: defaults) == .english)

        L10n.setLanguagePreference(.simplifiedChinese, defaults: defaults)
        precondition(L10n.languagePreference(defaults: defaults) == .simplifiedChinese)

        defaults.set("zh-TW", forKey: "AgentDockUILanguage")
        precondition(L10n.languagePreference(defaults: defaults) == .system)

        L10n.setLanguagePreference(.english, defaults: defaults)
        L10n.setLanguagePreference(.system, defaults: defaults)
        precondition(L10n.languagePreference(defaults: defaults) == .system)
        precondition(defaults.object(forKey: "AgentDockUILanguage") == nil)

        precondition(L10n.text("Change interface language?") != "")
        precondition(L10n.text("Changing the interface language restarts the AgentDock interface. Any unsaved changes in this window will be lost.") != "")
        precondition(L10n.text("Continue") != "")

        print("localization preference tests passed")
    }
}
