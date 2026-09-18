import AppKit
import OpenComputerUseKit

enum SDKPermissionOnboarding {
    static func request(_ ids: [String], cancellation: SDKCancellation) throws -> [[String: String]] {
        let permissions = try SDKPermissions.validateRequest(ids)
        let dismissed = DispatchSemaphore(value: 0)
        let controller = try DispatchQueue.main.sync { () throws -> PermissionWindowController? in
            guard !cancellation.isCancelled else {
                throw SDKDomainError("CANCELLED", "Permission request cancelled")
            }
            let observed = SDKPermissions.diagnostics()
            guard permissions.contains(where: { !observed.isGranted($0) }) else { return nil }
            NSApplication.shared.applicationIconImage = Branding.makeAppIconImage(size: 256)
            let controller = PermissionWindowController(
                terminateOnCompletion: false,
                permissions: permissions,
                onDismiss: { dismissed.signal() }
            )
            controller.showWindow(nil)
            NSApp.activate(ignoringOtherApps: true)
            controller.beginGuidance()
            return controller
        }
        guard let controller else { return SDKPermissions.status() }
        defer { DispatchQueue.main.sync { controller.close() } }
        while dismissed.wait(timeout: .now() + .milliseconds(50)) == .timedOut {
            guard !cancellation.isCancelled else {
                throw SDKDomainError("CANCELLED", "Permission onboarding cancelled", effect: "possible")
            }
        }
        return SDKPermissions.status()
    }
}
