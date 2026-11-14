import Foundation
import OpenComputerUseKit

final class SDKLockedUseProtection: SDKTaskProtection, @unchecked Sendable {
    private let connection: LockedUseConnection
    private var enabled = false

    init(clientSocket: Int32) { connection = LockedUseConnection(clientSocket: clientSocket) }

    func availability() -> [String: Any] {
        let diagnostics = LockedUseDiagnostics.current()
        return diagnostics.available ? ["status": "available"]
            : ["status": "unavailable", "reason": ["code": "PERMISSION_REQUIRED", "message": "Enable and validate Locked Use in Cherry Computer Use settings"]]
    }

    func configure(enabled: Bool) throws {
        if enabled, !LockedUseDiagnostics.current().available {
            throw SDKDomainError("PERMISSION_REQUIRED", "Enable and validate Locked Use in Cherry Computer Use settings before opting in")
        }
        self.enabled = enabled
    }

    func perform(cancellation: SDKCancellation, _ body: () throws -> Any) throws -> Any {
        if enabled { return try connection.perform(cancellation: cancellation, body) }
        try requireSDKUnlockedSession()
        return try body()
    }

    func close() throws { try connection.closeAndWait() }
}

private func requireSDKUnlockedSession() throws {
    guard LockedUseSession.current().state == .unlocked else {
        throw SDKDomainError("PERMISSION_REQUIRED", "This task has not opted in to Locked Use; unlock the Mac to continue")
    }
}
