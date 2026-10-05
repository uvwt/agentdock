import Darwin
import Foundation

enum DesktopUpdateTransactionState: String, Decodable {
    case staged
    case trial
    case committed
    case rollingBack = "rolling_back"
    case rolledBack = "rolled_back"
    case failed

    var isTerminal: Bool {
        switch self {
        case .committed, .rolledBack, .failed:
            return true
        case .staged, .trial, .rollingBack:
            return false
        }
    }

    var allowsAutomaticCleanup: Bool {
        self == .committed || self == .rolledBack
    }
}

fileprivate struct DesktopUpdateTransactionEnvelope: Decodable {
    let schemaVersion: Int
    let transactionID: String
    let platform: String
    let state: DesktopUpdateTransactionState
    let macOS: DesktopUpdateMacOSPlan?

    private enum CodingKeys: String, CodingKey {
        case schemaVersion = "schema_version"
        case transactionID = "transaction_id"
        case platform
        case state
        case macOS = "macos"
    }
}

fileprivate struct DesktopUpdateMacOSPlan: Decodable {
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
    enum InspectionKind: Equatable {
        case missing
        case nonTerminal
        case terminal
        case unreadable
    }

    struct Inspection {
        let kind: InspectionKind
        fileprivate let transaction: DesktopUpdateTransactionEnvelope?

        var needsFinishingUI: Bool {
            kind == .nonTerminal || kind == .unreadable
        }
    }

    enum Outcome: Equatable {
        case noTransaction
        case liveArbiter(transactionID: String)
        case terminal(transactionID: String, state: DesktopUpdateTransactionState)
        case recovered(transactionID: String, state: DesktopUpdateTransactionState)
        case blocked

        var requiresUpdateLock: Bool {
            switch self {
            case .liveArbiter, .blocked:
                return true
            case .noTransaction, .terminal, .recovered:
                return false
            }
        }

        var transactionID: String? {
            switch self {
            case .liveArbiter(let transactionID),
                 .terminal(let transactionID, _),
                 .recovered(let transactionID, _):
                return transactionID
            case .noTransaction, .blocked:
                return nil
            }
        }

        var allowsStaleTriggerCleanup: Bool {
            switch self {
            case .noTransaction, .terminal, .recovered:
                return true
            case .liveArbiter, .blocked:
                return false
            }
        }

        var allowsCoordinationCleanup: Bool {
            switch self {
            case .noTransaction:
                return true
            case .terminal(_, let state), .recovered(_, let state):
                return state.allowsAutomaticCleanup
            case .liveArbiter, .blocked:
                return false
            }
        }
    }

    private enum JournalError: LocalizedError {
        case unsupportedSchema(Int)
        case unsupportedPlatform(String)
        case missingTransactionID

        var errorDescription: String? {
            switch self {
            case .unsupportedSchema(let value):
                return "unsupported update transaction schema: \(value)"
            case .unsupportedPlatform(let value):
                return "unsupported update transaction platform: \(value)"
            case .missingTransactionID:
                return "update transaction id is missing"
            }
        }
    }

    static func inspect(paths: AppPaths) -> Inspection {
        guard FileManager.default.fileExists(atPath: paths.updateTransaction.path) else {
            return Inspection(kind: .missing, transaction: nil)
        }
        do {
            let transaction = try loadTransaction(paths: paths)
            return Inspection(
                kind: transaction.state.isTerminal ? .terminal : .nonTerminal,
                transaction: transaction
            )
        } catch {
            NSLog("AgentDock update recovery: durable transaction journal is unreadable: %@", error.localizedDescription)
            return Inspection(kind: .unreadable, transaction: nil)
        }
    }

    static func legacyCloudflaredRollbackSource(paths: AppPaths) -> URL? {
        guard let transaction = try? loadTransaction(paths: paths),
              transaction.state == .trial,
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

    static func recoverIfNeeded(
        paths: AppPaths,
        inspection: Inspection? = nil,
        recoveryTimeout: TimeInterval = 5 * 60
    ) async -> Outcome {
        let current = inspection ?? inspect(paths: paths)
        switch current.kind {
        case .missing:
            return .noTransaction
        case .terminal:
            guard let transaction = current.transaction else { return .blocked }
            return .terminal(transactionID: transaction.transactionID, state: transaction.state)
        case .unreadable:
            return .blocked
        case .nonTerminal:
            guard let transaction = current.transaction else { return .blocked }
            return await withCheckedContinuation { continuation in
                DispatchQueue.global(qos: .utility).async {
                    continuation.resume(returning: recoverNonTerminalTransaction(
                        paths: paths,
                        transaction: transaction,
                        recoveryTimeout: recoveryTimeout
                    ))
                }
            }
        }
    }

    private static func loadTransaction(paths: AppPaths) throws -> DesktopUpdateTransactionEnvelope {
        let data = try Data(contentsOf: paths.updateTransaction)
        let transaction = try JSONDecoder().decode(DesktopUpdateTransactionEnvelope.self, from: data)
        guard transaction.schemaVersion == 1 else {
            throw JournalError.unsupportedSchema(transaction.schemaVersion)
        }
        guard transaction.platform == "darwin" else {
            throw JournalError.unsupportedPlatform(transaction.platform)
        }
        guard !transaction.transactionID.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else {
            throw JournalError.missingTransactionID
        }
        return transaction
    }

    private static func recoverNonTerminalTransaction(
        paths: AppPaths,
        transaction: DesktopUpdateTransactionEnvelope,
        recoveryTimeout: TimeInterval
    ) -> Outcome {
        guard let plan = transaction.macOS else {
            NSLog("AgentDock update recovery: non-terminal macOS transaction has no platform plan.")
            return .blocked
        }

        let allowedRoot = paths.appSupport
            .appendingPathComponent("update/arbiters", isDirectory: true)
            .standardizedFileURL
            .path
        let transactionRoot = URL(fileURLWithPath: allowedRoot, isDirectory: true)
            .appendingPathComponent(transaction.transactionID, isDirectory: true)
            .path
        guard let resolvedAllowedRoot = canonicalPath(allowedRoot),
              let resolvedTransactionRoot = canonicalPath(transactionRoot),
              let resolvedArbiter = canonicalPath(plan.sourceArbiterPath) else {
            NSLog("AgentDock update recovery: source Arbiter path could not be resolved safely.")
            return .blocked
        }

        let allowedPrefix = resolvedAllowedRoot.hasSuffix("/") ? resolvedAllowedRoot : resolvedAllowedRoot + "/"
        let expectedArbiter = URL(fileURLWithPath: resolvedTransactionRoot, isDirectory: true)
            .appendingPathComponent("agentdock-arbiter")
            .path
        guard resolvedTransactionRoot.hasPrefix(allowedPrefix),
              resolvedArbiter == expectedArbiter,
              FileManager.default.isExecutableFile(atPath: resolvedArbiter) else {
            NSLog("AgentDock update recovery: source Arbiter path is not trusted: %@", resolvedArbiter)
            return .blocked
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
            return .blocked
        }

        let deadline = Date().addingTimeInterval(max(0, recoveryTimeout))
        while process.isRunning, Date() < deadline {
            Thread.sleep(forTimeInterval: 0.1)
        }
        if process.isRunning {
            process.terminate()
            NSLog("AgentDock update recovery: source Arbiter exceeded the recovery deadline.")
            return outcomeAfterArbiterExit(
                paths: paths,
                transactionID: transaction.transactionID,
                terminationStatus: nil
            )
        }

        return outcomeAfterArbiterExit(
            paths: paths,
            transactionID: transaction.transactionID,
            terminationStatus: process.terminationStatus
        )
    }

    private static func outcomeAfterArbiterExit(
        paths: AppPaths,
        transactionID: String,
        terminationStatus: Int32?
    ) -> Outcome {
        let final = inspect(paths: paths)
        guard let transaction = final.transaction,
              transaction.transactionID == transactionID else {
            NSLog("AgentDock update recovery: durable transaction changed or disappeared during recovery.")
            return .blocked
        }

        if transaction.state.isTerminal {
            // transaction.json 是 durable commit point。rollback 成功时 Arbiter 会保留原始
            // trial error 并以非零状态退出，因此 terminal journal 必须优先于进程退出码。
            return .recovered(transactionID: transaction.transactionID, state: transaction.state)
        }

        if terminationStatus == 0 {
            // --recover-if-unlocked 在原 Arbiter 仍持有 transaction.lock 时会正常 exit 0。
            // journal 继续保持 staged/trial/rolling_back 正是健康的 live-update 路径。
            return .liveArbiter(transactionID: transaction.transactionID)
        }

        if let terminationStatus {
            NSLog(
                "AgentDock update recovery: source Arbiter exited with status %d while transaction remained %@.",
                terminationStatus,
                transaction.state.rawValue
            )
        }
        return .blocked
    }

    private static func canonicalPath(_ path: String) -> String? {
        path.withCString { rawPath in
            guard let resolved = Darwin.realpath(rawPath, nil) else { return nil }
            defer { free(resolved) }
            return String(cString: resolved)
        }
    }
}
