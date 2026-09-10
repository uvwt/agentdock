import Foundation

final class LegacyDesktopRuntimeMigration {
    private static let services = [
        (label: "com.uvwt.agentdock", plist: "com.uvwt.agentdock.plist"),
        (label: "com.uvwt.agentdock.cloudflared", plist: "com.uvwt.agentdock.cloudflared.plist"),
        (label: "com.uvwt.agentdock.menu", plist: "com.uvwt.agentdock.menu.plist"),
    ]

    private let paths: AppPaths
    private let fileManager: FileManager

    init(paths: AppPaths, fileManager: FileManager = .default) {
        self.paths = paths
        self.fileManager = fileManager
    }

    static func isPresent(paths: AppPaths, fileManager: FileManager = .default) -> Bool {
        let launchAgents = paths.home.appendingPathComponent("Library/LaunchAgents")
        return services.contains { service in
            fileManager.fileExists(atPath: launchAgents.appendingPathComponent(service.plist).path)
                || launchdLoaded(label: service.label)
        }
    }

    func begin() throws -> Transaction? {
        guard Self.isPresent(paths: paths, fileManager: fileManager) else { return nil }

        let domain = Self.launchdDomain
        let loadedLabels = Self.services.compactMap { service in
            Self.launchdLoaded(label: service.label) ? service.label : nil
        }
        for label in loadedLabels {
            let result = try runProcess(
                executable: "/bin/launchctl",
                arguments: ["bootout", "\(domain)/\(label)"]
            )
            guard result.status == 0 || !Self.launchdLoaded(label: label) else {
                throw ValidationError(L10n.format("Unable to stop legacy AgentDock background service: %@", label))
            }
        }

        let backupRoot = paths.appSupport
            .appendingPathComponent(".legacy-runtime-migration-\(UUID().uuidString)", isDirectory: true)
        try fileManager.createDirectory(at: backupRoot, withIntermediateDirectories: true)
        try fileManager.setAttributes([.posixPermissions: 0o700], ofItemAtPath: backupRoot.path)

        var moved: [(original: URL, backup: URL)] = []
        do {
            for (index, original) in managedFiles().enumerated() where fileManager.fileExists(atPath: original.path) {
                let values = try original.resourceValues(forKeys: [.isRegularFileKey, .isSymbolicLinkKey])
                guard values.isRegularFile == true || values.isSymbolicLink == true else {
                    throw ValidationError(L10n.format("Legacy AgentDock runtime file is not a regular file: %@", original.path))
                }
                let backup = backupRoot.appendingPathComponent("\(index)-\(original.lastPathComponent)")
                try fileManager.moveItem(at: original, to: backup)
                moved.append((original, backup))
            }
        } catch {
            try? Self.restoreMovedFiles(moved.reversed(), fileManager: fileManager)
            try? fileManager.removeItem(at: backupRoot)
            try? Self.restoreLoadedServices(loadedLabels, paths: paths)
            throw error
        }

        return Transaction(
            paths: paths,
            backupRoot: backupRoot,
            moved: moved,
            loadedLabels: loadedLabels,
            fileManager: fileManager
        )
    }

    private func managedFiles() -> [URL] {
        let launchAgents = paths.home.appendingPathComponent("Library/LaunchAgents")
        return [
            paths.home.appendingPathComponent(".local/bin/agentdock"),
            paths.home.appendingPathComponent(".local/bin/cloudflared"),
            launchAgents.appendingPathComponent("com.uvwt.agentdock.plist"),
            launchAgents.appendingPathComponent("com.uvwt.agentdock.cloudflared.plist"),
            launchAgents.appendingPathComponent("com.uvwt.agentdock.menu.plist"),
            paths.appSupport.appendingPathComponent("start-agentdock.sh"),
            paths.appSupport.appendingPathComponent("start-cloudflared.sh"),
        ]
    }

    final class Transaction {
        private let paths: AppPaths
        private let backupRoot: URL
        private let moved: [(original: URL, backup: URL)]
        private let loadedLabels: [String]
        private let fileManager: FileManager
        private var finished = false

        fileprivate init(
            paths: AppPaths,
            backupRoot: URL,
            moved: [(original: URL, backup: URL)],
            loadedLabels: [String],
            fileManager: FileManager
        ) {
            self.paths = paths
            self.backupRoot = backupRoot
            self.moved = moved
            self.loadedLabels = loadedLabels
            self.fileManager = fileManager
        }

        func commit() throws {
            guard !finished else { return }
            try fileManager.removeItem(at: backupRoot)
            finished = true
        }

        func rollback() throws {
            guard !finished else { return }
            try LegacyDesktopRuntimeMigration.restoreMovedFiles(moved.reversed(), fileManager: fileManager)
            try? fileManager.removeItem(at: backupRoot)
            try LegacyDesktopRuntimeMigration.restoreLoadedServices(loadedLabels, paths: paths)
            finished = true
        }

        deinit {
            if !finished {
                try? rollback()
            }
        }
    }

    private static var launchdDomain: String { "gui/\(getuid())" }

    private static func launchdLoaded(label: String) -> Bool {
        guard let result = try? runProcess(
            executable: "/bin/launchctl",
            arguments: ["print", "\(launchdDomain)/\(label)"]
        ) else { return false }
        return result.status == 0
    }

    private static func restoreMovedFiles<S: Sequence>(
        _ moved: S,
        fileManager: FileManager
    ) throws where S.Element == (original: URL, backup: URL) {
        for item in moved where fileManager.fileExists(atPath: item.backup.path) {
            try fileManager.createDirectory(
                at: item.original.deletingLastPathComponent(),
                withIntermediateDirectories: true
            )
            if fileManager.fileExists(atPath: item.original.path) {
                try fileManager.removeItem(at: item.original)
            }
            try fileManager.moveItem(at: item.backup, to: item.original)
        }
    }

    private static func restoreLoadedServices(_ labels: [String], paths: AppPaths) throws {
        let launchAgents = paths.home.appendingPathComponent("Library/LaunchAgents")
        for label in labels {
            guard let service = services.first(where: { $0.label == label }) else { continue }
            let plist = launchAgents.appendingPathComponent(service.plist)
            guard FileManager.default.fileExists(atPath: plist.path) else { continue }
            let bootstrap = try runProcess(
                executable: "/bin/launchctl",
                arguments: ["bootstrap", launchdDomain, plist.path]
            )
            guard bootstrap.status == 0 || launchdLoaded(label: label) else {
                throw ValidationError(L10n.format("Unable to restore legacy AgentDock background service: %@", label))
            }
            _ = try? runProcess(
                executable: "/bin/launchctl",
                arguments: ["kickstart", "-k", "\(launchdDomain)/\(label)"]
            )
        }
    }
}
