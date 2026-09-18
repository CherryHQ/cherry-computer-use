import ApplicationServices

public enum SDKPermissions {
    public static func diagnostics() -> PermissionDiagnostics {
        PermissionDiagnostics(
            accessibilityTrusted: AXIsProcessTrusted(),
            screenCaptureGranted: CGPreflightScreenCaptureAccess()
        )
    }

    public static func status() -> [[String: String]] {
        let observed = diagnostics()
        return SystemPermissionKind.allCases.map { permission in
            let granted = observed.isGranted(permission)
            return ["id": permission.rawValue,
                    "label": permission == .accessibility ? "Accessibility" : "Screen Recording",
                    "status": granted ? "granted" : "unknown",
                    "interaction": granted ? "none" : "systemSettings"]
        }
    }

    public static func validateRequest(_ ids: [String]) throws -> [SystemPermissionKind] {
        try ids.map { id in
            guard let permission = SystemPermissionKind(rawValue: id) else {
                throw SDKDomainError("INVALID_ARGUMENT", "Unknown permission: \(id)")
            }
            return permission
        }
    }

    static func request(_ ids: [String], cancellation: SDKCancellation) throws -> [[String: String]] {
        _ = try validateRequest(ids)
        throw SDKDomainError("UNSUPPORTED_CAPABILITY", "Permission onboarding requires the macOS app runtime")
    }
}
