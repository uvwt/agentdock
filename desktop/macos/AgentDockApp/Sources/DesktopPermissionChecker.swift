import AppKit
import ApplicationServices
import CoreGraphics
import CoreServices
import Foundation

enum DesktopPermissionState: Equatable {
    case granted
    case notGranted
    case notDetermined
    case unavailable

    var title: String {
        switch self {
        case .granted: return L10n.text("Granted")
        case .notGranted: return L10n.text("Not granted")
        case .notDetermined: return L10n.text("Not determined")
        case .unavailable: return L10n.text("Unable to check")
        }
    }
}

enum DesktopPermissionKind: Int, CaseIterable {
    case accessibility
    case screenRecording
    case systemEventsAutomation
    case finderAutomation

    var title: String {
        switch self {
        case .accessibility: return L10n.text("Accessibility")
        case .screenRecording: return L10n.text("Screen Recording")
        case .systemEventsAutomation: return L10n.text("Automation · System Events")
        case .finderAutomation: return L10n.text("Automation · Finder")
        }
    }

    var detail: String {
        switch self {
        case .accessibility:
            return L10n.text("Used for keyboard, mouse, and accessibility-protected UI operations.")
        case .screenRecording:
            return L10n.text("Used for screenshots and reading screen content.")
        case .systemEventsAutomation:
            return L10n.text("Used for system-level automation through Apple Events.")
        case .finderAutomation:
            return L10n.text("Used to control Finder through Apple Events.")
        }
    }

    var settingsPane: String {
        switch self {
        case .accessibility: return "Privacy_Accessibility"
        case .screenRecording: return "Privacy_ScreenCapture"
        case .systemEventsAutomation, .finderAutomation: return "Privacy_Automation"
        }
    }
}

struct DesktopPermissionSnapshot {
    let states: [DesktopPermissionKind: DesktopPermissionState]

    subscript(_ kind: DesktopPermissionKind) -> DesktopPermissionState {
        states[kind] ?? .unavailable
    }
}

enum DesktopPermissionChecker {
    static func snapshot() -> DesktopPermissionSnapshot {
        DesktopPermissionSnapshot(states: [
            .accessibility: AXIsProcessTrusted() ? .granted : .notGranted,
            .screenRecording: CGPreflightScreenCaptureAccess() ? .granted : .notGranted,
            .systemEventsAutomation: automationState(bundleID: "com.apple.systemevents"),
            .finderAutomation: automationState(bundleID: "com.apple.finder"),
        ])
    }

    static func request(_ kind: DesktopPermissionKind) {
        switch kind {
        case .accessibility:
            let promptKey = kAXTrustedCheckOptionPrompt.takeUnretainedValue() as String
            _ = AXIsProcessTrustedWithOptions([promptKey: true] as CFDictionary)
        case .screenRecording:
            _ = CGRequestScreenCaptureAccess()
        case .systemEventsAutomation:
            requestAutomation(script: "tell application \"System Events\" to get name of current user")
        case .finderAutomation:
            requestAutomation(script: "tell application \"Finder\" to get name of startup disk")
        }
    }

    static func openSettings(for kind: DesktopPermissionKind) {
        openPrivacySettings(pane: kind.settingsPane)
    }

    static func openFullDiskAccessSettings() {
        openPrivacySettings(pane: "Privacy_AllFiles")
    }

    static func openAppManagementSettings() {
        openPrivacySettings(pane: "Privacy_AppBundles")
    }

    static func openFilesAndFoldersSettings() {
        openPrivacySettings(pane: "Privacy_FilesAndFolders")
    }

    private static func automationState(bundleID: String) -> DesktopPermissionState {
        var target = AEAddressDesc()
        guard let data = bundleID.data(using: .utf8) else { return .unavailable }
        let createStatus = data.withUnsafeBytes { bytes in
            AECreateDesc(DescType(typeApplicationBundleID), bytes.baseAddress, data.count, &target)
        }
        guard createStatus == noErr else { return .unavailable }
        defer { AEDisposeDesc(&target) }

        let status = AEDeterminePermissionToAutomateTarget(
            &target,
            AEEventClass(kCoreEventClass),
            AEEventID(kAEGetData),
            false
        )
        switch status {
        case noErr:
            return .granted
        case OSStatus(errAEEventNotPermitted):
            return .notGranted
        case OSStatus(errAEEventWouldRequireUserConsent), OSStatus(procNotFound):
            // 目标进程尚未运行时系统也会返回 procNotFound；此时不能推断为拒绝。
            return .notDetermined
        default:
            return .unavailable
        }
    }

    private static func requestAutomation(script: String) {
        var error: NSDictionary?
        _ = NSAppleScript(source: script)?.executeAndReturnError(&error)
    }

    private static func openPrivacySettings(pane: String) {
        guard let url = URL(string: "x-apple.systempreferences:com.apple.preference.security?\(pane)") else { return }
        NSWorkspace.shared.open(url)
    }
}
