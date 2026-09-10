import Foundation
import ServiceManagement

final class MenuLoginAgentController {
    private let menuAgentPlistName = "com.uvwt.agentdock.menu-login.plist"
    private let legacyHelperIdentifier = "com.uvwt.agentdock.login-helper"
    private let preferenceKey = "menuLoginEnabled"
    private let preferenceInitializedKey = "menuLoginPreferenceInitialized"
    private let defaults: UserDefaults

    init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
    }

    private var menuAgent: SMAppService {
        SMAppService.agent(plistName: menuAgentPlistName)
    }

    func configureOnLaunch() throws {
        guard !isRunningFromTransientVolume else { return }
        initializePreferenceIfNeeded()
        if defaults.bool(forKey: preferenceKey) {
            try register()
        } else {
            try unregister()
        }
        migrateLegacyRegistrationsIfSafe()
    }

    func restoreAfterUpdate() throws {
        guard !isRunningFromTransientVolume else { return }
        initializePreferenceIfNeeded()
        guard defaults.bool(forKey: preferenceKey) else {
            try unregister()
            migrateLegacyRegistrationsIfSafe()
            return
        }

        try validateBundledAgent()
        switch menuAgent.status {
        case .enabled:
            try menuAgent.unregister()
            try menuAgent.register()
        case .requiresApproval:
            break
        case .notFound, .notRegistered:
            try menuAgent.register()
        @unknown default:
            try menuAgent.register()
        }
        migrateLegacyRegistrationsIfSafe()
    }

    var isEnabled: Bool {
        menuAgent.status == .enabled || menuAgent.status == .requiresApproval
    }

    func setEnabled(_ enabled: Bool) throws {
        if enabled {
            try register()
        } else {
            try unregister()
        }
        defaults.set(true, forKey: preferenceInitializedKey)
        defaults.set(enabled, forKey: preferenceKey)
        migrateLegacyRegistrationsIfSafe()
    }

    func register() throws {
        guard !isRunningFromTransientVolume else {
            throw ValidationError(L10n.text("Move AgentDock to the Applications folder before enabling launch at sign-in."))
        }
        try validateBundledAgent()
        switch menuAgent.status {
        case .enabled, .requiresApproval:
            return
        case .notFound, .notRegistered:
            try menuAgent.register()
        @unknown default:
            try menuAgent.register()
        }
    }

    func unregister() throws {
        if menuAgent.status == .enabled || menuAgent.status == .requiresApproval {
            try menuAgent.unregister()
        }
    }

    private func initializePreferenceIfNeeded() {
        guard !defaults.bool(forKey: preferenceInitializedKey) else { return }
        defaults.set(true, forKey: preferenceInitializedKey)
        defaults.set(true, forKey: preferenceKey)
    }

    private func migrateLegacyRegistrationsIfSafe() {
        // 用户希望保留登录启动时，只有新 agent 已真正启用才清理旧注册。
        // 若系统仍要求批准，先保留旧登录项，避免迁移过程中出现自动启动空窗。
        if defaults.bool(forKey: preferenceKey), menuAgent.status != .enabled {
            return
        }

        let obsoleteMainApp = SMAppService.mainApp
        if obsoleteMainApp.status == .enabled || obsoleteMainApp.status == .requiresApproval {
            try? obsoleteMainApp.unregister()
        }

        let legacyLoginItem = SMAppService.loginItem(identifier: legacyHelperIdentifier)
        if legacyLoginItem.status == .enabled || legacyLoginItem.status == .requiresApproval {
            try? legacyLoginItem.unregister()
        }
    }

    private func validateBundledAgent() throws {
        let appBundle = Bundle.main.bundleURL
        let plist = appBundle.appendingPathComponent("Contents/Library/LaunchAgents/\(menuAgentPlistName)")
        let helper = appBundle.appendingPathComponent("Contents/Helpers/AgentDockLoginHelper")
        let fileManager = FileManager.default

        var isDirectory: ObjCBool = false
        guard fileManager.fileExists(atPath: plist.path, isDirectory: &isDirectory), !isDirectory.boolValue else {
            throw ValidationError(L10n.text("AgentDock app bundle is missing the menu bar login service definition."))
        }
        guard fileManager.fileExists(atPath: helper.path, isDirectory: &isDirectory),
              !isDirectory.boolValue,
              fileManager.isExecutableFile(atPath: helper.path) else {
            throw ValidationError(L10n.text("AgentDock app bundle is missing an executable menu bar login component."))
        }
    }

    private var isRunningFromTransientVolume: Bool {
        let path = Bundle.main.bundleURL.resolvingSymlinksInPath().path
        return path == "/Volumes" || path.hasPrefix("/Volumes/")
    }
}
