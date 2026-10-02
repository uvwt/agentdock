import Darwin
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

    private enum CodingKeys: String, CodingKey {
        case sourceArbiterPath = "source_arbiter_path"
    }
}

enum DesktopUpdateTransactionRecovery {
    static func hasActiveTransaction(paths: AppPaths) -> Bool {
        pendingTransaction(paths: paths) != nil
    }

    static func shouldPreserveResultTrigger(paths: AppPaths) -> Bool {
        guard FileManager.default.fileExists(atPath: paths.updateTransaction.path) else { return false }
        guard let transaction = loadTransaction(paths: paths) else {
            // An unreadable durable journal is a recovery problem, not proof that the
            // one-shot desktop trigger is stale.
            return true
        }
        return transaction.state == "trial" || transaction.state == "rolling_back"
    }

    static func recoverIfNeeded(
        paths: AppPaths,
        recoveryTimeout: TimeInterval = 90,
        journalSettleTimeout: TimeInterval = 2
    ) async -> Bool {
        let transactionFileExists = FileManager.default.fileExists(atPath: paths.updateTransaction.path)
        guard !transactionFileExists || loadTransaction(paths: paths) != nil else {
            NSLog("AgentDock update recovery: durable transaction journal is unreadable.")
            return false
        }
        guard hasActiveTransaction(paths: paths) else { return true }
        return await withCheckedContinuation { continuation in
            DispatchQueue.global(qos: .utility).async {
                continuation.resume(returning: recoverSynchronouslyIfNeeded(
                    paths: paths,
                    recoveryTimeout: recoveryTimeout,
                    journalSettleTimeout: journalSettleTimeout
                ))
            }
        }
    }

    private static func loadTransaction(paths: AppPaths) -> DesktopUpdateTransactionEnvelope? {
        guard let data = try? Data(contentsOf: paths.updateTransaction),
              let transaction = try? JSONDecoder().decode(DesktopUpdateTransactionEnvelope.self, from: data),
              transaction.schemaVersion == 1 else {
            return nil
        }
        return transaction
    }

    private static func pendingTransaction(paths: AppPaths) -> DesktopUpdateTransactionEnvelope? {
        guard let transaction = loadTransaction(paths: paths),
              transaction.state == "trial" || transaction.state == "rolling_back" else {
            return nil
        }
        return transaction
    }

    private static func recoverSynchronouslyIfNeeded(
        paths: AppPaths,
        recoveryTimeout: TimeInterval,
        journalSettleTimeout: TimeInterval
    ) -> Bool {
        guard let transaction = pendingTransaction(paths: paths) else { return true }
        guard let plan = transaction.macOS else {
            NSLog("AgentDock update recovery: pending macOS transaction has no platform plan.")
            return false
        }

        let allowedRoot = paths.appSupport
            .appendingPathComponent("update/arbiters", isDirectory: true)
            .standardizedFileURL
            .path
        guard let resolvedAllowedRoot = canonicalPath(allowedRoot),
              let resolvedArbiter = canonicalPath(plan.sourceArbiterPath) else {
            NSLog("AgentDock update recovery: source Arbiter path could not be resolved safely.")
            return false
        }
        let prefix = resolvedAllowedRoot.hasSuffix("/") ? resolvedAllowedRoot : resolvedAllowedRoot + "/"
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

        let deadline = Date().addingTimeInterval(max(0, recoveryTimeout))
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

        // Exit status alone is not enough. The durable journal is the source of truth: a
        // helper that exits 0 without resolving trial/rolling_back must not let the new App
        // acknowledge the handoff and clear update coordination files.
        let settleDeadline = Date().addingTimeInterval(max(0, journalSettleTimeout))
        repeat {
            if durableTransactionIsSettled(paths: paths) {
                return true
            }
            if Date() >= settleDeadline {
                break
            }
            Thread.sleep(forTimeInterval: 0.05)
        } while true

        NSLog("AgentDock update recovery: source Arbiter exited successfully but the durable transaction did not settle.")
        return false
    }

    private static func canonicalPath(_ path: String) -> String? {
        path.withCString { rawPath in
            guard let resolved = Darwin.realpath(rawPath, nil) else { return nil }
            defer { free(resolved) }
            return String(cString: resolved)
        }
    }

    private static func durableTransactionIsSettled(paths: AppPaths) -> Bool {
        let fileManager = FileManager.default
        guard fileManager.fileExists(atPath: paths.updateTransaction.path) else { return true }
        guard let transaction = loadTransaction(paths: paths) else { return false }
        return transaction.state != "trial" && transaction.state != "rolling_back"
    }
}
