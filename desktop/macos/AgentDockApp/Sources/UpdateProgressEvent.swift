import Foundation

enum UpdateProgressStage: String, Decodable, Sendable {
    case checking
    case downloading
    case verifying
    case extracting
    case installing
    case updatingSkills = "updating_skills"
    case restarting
}

enum UpdateProgressEventType: String, Decodable, Sendable {
    case stage
    case progress
    case completed
    case failed
}

struct UpdateProgressEvent: Decodable, Sendable {
    let schemaVersion: Int
    let type: UpdateProgressEventType
    let stage: UpdateProgressStage?
    let currentVersion: String?
    let targetVersion: String?
    let asset: String?
    let bytes: Int64?
    let totalBytes: Int64?
    let error: String?

    private enum CodingKeys: String, CodingKey {
        case schemaVersion = "schema_version"
        case type
        case stage
        case currentVersion = "current_version"
        case targetVersion = "target_version"
        case asset
        case bytes
        case totalBytes = "total_bytes"
        case error
    }

    static func local(
        type: UpdateProgressEventType,
        stage: UpdateProgressStage? = nil,
        currentVersion: String? = nil,
        targetVersion: String? = nil,
        error: String? = nil
    ) -> UpdateProgressEvent {
        UpdateProgressEvent(
            schemaVersion: 1,
            type: type,
            stage: stage,
            currentVersion: currentVersion,
            targetVersion: targetVersion,
            asset: nil,
            bytes: nil,
            totalBytes: nil,
            error: error
        )
    }
}
