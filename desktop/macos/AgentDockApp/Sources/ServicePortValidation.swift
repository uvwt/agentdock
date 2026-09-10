import Foundation

enum ServicePortValidation {
    static let allowedRange = 1024...65535

    static func validate(_ port: Int) throws {
        guard allowedRange.contains(port) else {
            throw ValidationError(L10n.text("The service port for a standard user must be between 1024 and 65535."))
        }
    }
}
