import AppKit
import Darwin
import Foundation

if CommandLine.arguments.contains("--unregister-background-services") {
    var failures: [String] = []
    MainActor.assumeIsolated {
        do {
            try ServiceController().unregisterManagedBackgroundServicesForUninstall()
        } catch {
            failures.append(error.localizedDescription)
        }
        do {
            try MenuLoginAgentController().unregisterForUninstall()
        } catch {
            failures.append(error.localizedDescription)
        }
    }
    if failures.isEmpty {
        exit(0)
    }
    let message = failures.joined(separator: "\n") + "\n"
    FileHandle.standardError.write(Data(message.utf8))
    exit(1)
}

MainActor.assumeIsolated {
    let application = NSApplication.shared
    ApplicationMenu.install()
    let delegate = AppDelegate()
    application.delegate = delegate
    application.setActivationPolicy(.accessory)
    application.run()
}
