import Foundation

enum FileAccessState: Equatable {
    case notChecked
    case accessible
    case denied
    case missing
    case unavailable

    var title: String {
        switch self {
        case .notChecked: return L10n.text("Not checked")
        case .accessible: return L10n.text("Accessible")
        case .denied: return L10n.text("No access")
        case .missing: return L10n.text("Folder does not exist")
        case .unavailable: return L10n.text("Unable to check")
        }
    }
}

struct FileAccessCheck: Equatable {
    let title: String
    let url: URL
    let state: FileAccessState
}

enum FileAccessPermissionChecker {
    static func uncheckedStandardLocations(
        home: URL = FileManager.default.homeDirectoryForCurrentUser
    ) -> [FileAccessCheck] {
        standardTargets(home: home).map {
            FileAccessCheck(title: $0.title, url: $0.url, state: .notChecked)
        }
    }

    static func standardLocations(home: URL = FileManager.default.homeDirectoryForCurrentUser) -> [FileAccessCheck] {
        standardTargets(home: home).map { check(title: $0.title, url: $0.url) }
    }

    static func check(title: String, url: URL) -> FileAccessCheck {
        var isDirectory: ObjCBool = false
        guard FileManager.default.fileExists(atPath: url.path, isDirectory: &isDirectory), isDirectory.boolValue else {
            return FileAccessCheck(title: title, url: url, state: .missing)
        }
        do {
            _ = try FileManager.default.contentsOfDirectory(at: url, includingPropertiesForKeys: nil, options: [])
            return FileAccessCheck(title: title, url: url, state: .accessible)
        } catch let error as NSError {
            if error.domain == NSPOSIXErrorDomain,
               error.code == Int(EACCES) || error.code == Int(EPERM) {
                return FileAccessCheck(title: title, url: url, state: .denied)
            }
            return FileAccessCheck(title: title, url: url, state: .unavailable)
        }
    }

    private static func standardTargets(home: URL) -> [(title: String, url: URL)] {
        [
            (L10n.text("Desktop"), home.appendingPathComponent("Desktop", isDirectory: true)),
            (L10n.text("Documents"), home.appendingPathComponent("Documents", isDirectory: true)),
            (L10n.text("Downloads"), home.appendingPathComponent("Downloads", isDirectory: true)),
        ]
    }
}
