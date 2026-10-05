import Foundation

private struct DesktopUpdateTransactionEnvelope: Decodable {
    let schemaVersion: Int
    let transactionID: String
    let state: String
    let macOS: DesktopUpdateMacOSPlan?

    private enum CodingKeys: String, CodingKey {
        case schemaVersion = "schema_version"
        case transactionID = "transaction_id"
        case state
        case macOS = "macos"
    }
}

private struct DesktopUpdateMacOSPlan: Decodable {
    let sourceArbiterPath: String
    let targetAppPath: String?
    let trialAppPath: String?

    private enum CodingKeys: String, CodingKey {
        case sourceArbiterPath = "source_arbiter_path"
        case targetAppPath = "target_app_path"
        case trialAppPath = "trial_app_path"
    }
}

enum DesktopUpdateTransactionRecovery {
    static func legacyCloudflaredRollbackSource(paths: AppPaths) -> URL? {
        guard let data = try? Data(contentsOf: paths.updateTransaction),
              let transaction = try? JSONDecoder().decode(DesktopUpdateTransactionEnvelope.self, from: data),
              transaction.schemaVersion == 1,
              transaction.state == "trial",
              let plan = transaction.macOS,
              let targetPath = plan.targetAppPath,
              let trialPath = plan.trialAppPath else {
            return nil
        }

        let current = paths.appBundle.standardizedFileURL.resolvingSymlinksInPath()
        let target = URL(fileURLWithPath: targetPath).standardizedFileURL.resolvingSymlinksInPath()
        let trial = URL(fileURLWithPath: trialPath).standardizedFileURL
        guard target.path == current.path,
              trial.deletingLastPathComponent().path == target.deletingLastPathComponent().path,
              trial.lastPathComponent.hasPrefix(".AgentDock.app.trial.") else {
            return nil
        }
        return trial.appendingPathComponent("Contents/Helpers/cloudflared")
    }

    static func recoverIfNeeded(paths: AppPaths) -> Bool {
        guard let data = try? Data(contentsOf: paths.updateTransaction),
              let transaction = try? JSONDecoder().decode(DesktopUpdateTransactionEnvelope.self, from: data),
              transaction.schemaVersion == 1,
              transaction.state == "trial" || transaction.state == "rolling_back" else {
            return true
        }
        guard let plan = transaction.macOS else {
            NSLog("AgentDock update recovery: pending macOS transaction has no platform plan.")
            return false
        }

        let allowedRoot = paths.appSupport
            .appendingPathComponent("update/arbiters", isDirectory: true)
            .standardizedFileURL
            .path
        let arbiter = URL(fileURLWithPath: plan.sourceArbiterPath).standardizedFileURL
        let resolvedArbiter = arbiter.resolvingSymlinksInPath().path
        let prefix = allowedRoot.hasSuffix("/") ? allowedRoot : allowedRoot + "/"
        guard resolvedArbiter.hasPrefix(prefix),
              FileManager.default.isExecutableFile(atPath: resolvedArbiter) else {
            NSLog("AgentDock update recovery: source Arbiter path is not trusted: %@", resolvedArbiter)
            return false
        }

        let process = Process()
        process.executableURL = URL(fileURLWithPath: resolvedArbiter)
        process.arguments = [
            "--root", paths.appSupport.path,
            "--transaction-id", transaction.transactionID,
            "--recover-if-unlocked",
        ]
        process.standardOutput = FileHandle.nullDevice
        process.standardError = FileHandle.nullDevice
        do {
            try process.run()
        } catch {
            NSLog("AgentDock update recovery: unable to start source Arbiter: %@", error.localizedDescription)
            return false
        }

        let deadline = Date().addingTimeInterval(90)
        while process.isRunning, Date() < deadline {
            Thread.sleep(forTimeInterval: 0.1)
        }
        if process.isRunning {
            process.terminate()
            NSLog("AgentDock update recovery: source Arbiter exceeded the recovery deadline.")
            return false
        }
        if process.terminationStatus != 0 {
            // Successful rollback intentionally returns the original trial error. In that case
            // the Arbiter launches the source App and this trial process is normally terminated.
            NSLog("AgentDock update recovery: source Arbiter exited with status %d.", process.terminationStatus)
            return false
        }
        return true
    }
}
