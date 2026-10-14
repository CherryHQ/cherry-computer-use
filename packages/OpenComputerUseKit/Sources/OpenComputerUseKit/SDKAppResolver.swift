import AppKit
import Foundation

func resolveSDKApp(_ query: String, launch: Bool, cancellation: SDKCancellation) throws -> RunningAppDescriptor {
    let query = query.trimmingCharacters(in: .whitespacesAndNewlines)
    func matches() -> [RunningAppDescriptor] {
        AppDiscovery.runningApps().filter {
            $0.name.caseInsensitiveCompare(query) == .orderedSame ||
            $0.bundleIdentifier?.caseInsensitiveCompare(query) == .orderedSame ||
            $0.runningApplication.bundleURL?.path == query
        }
    }
    func allowed(_ app: RunningAppDescriptor) throws -> RunningAppDescriptor {
        guard !AppSafetyPolicy.isBlocked(bundleIdentifier: app.bundleIdentifier) else {
            throw SDKDomainError("PERMISSION_REQUIRED", "Application is excluded from Computer Use")
        }
        return app
    }
    let running = matches()
    guard running.count <= 1 else { throw SDKDomainError("TARGET_UNAVAILABLE", "Application name is ambiguous: " + running.map { $0.bundleIdentifier ?? $0.name }.joined(separator: ", ") + ". Use a bundle ID or path") }
    if let app = running.first { return try allowed(app) }
    guard launch else { throw SDKDomainError("TARGET_UNAVAILABLE", "Application is no longer running; bind it explicitly to launch again") }
    var candidates = Set<URL>()
    if query.hasPrefix("/") && query.hasSuffix(".app") {
        candidates.insert(URL(fileURLWithPath: query).standardizedFileURL)
    } else if let url = NSWorkspace.shared.urlForApplication(withBundleIdentifier: query) {
        candidates.insert(url.standardizedFileURL)
    } else {
        let roots = ["/Applications", "/System/Applications", FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent("Applications").path]
        for root in roots {
            guard let entries = FileManager.default.enumerator(at: URL(fileURLWithPath: root), includingPropertiesForKeys: nil, options: [.skipsHiddenFiles, .skipsPackageDescendants]) else { continue }
            for case let url as URL in entries where url.pathExtension == "app" {
                if url.deletingPathExtension().lastPathComponent.caseInsensitiveCompare(query.replacingOccurrences(of: ".app", with: "")) == .orderedSame {
                    candidates.insert(url.standardizedFileURL)
                }
            }
        }
    }
    if candidates.count > 1 {
        throw SDKDomainError("TARGET_UNAVAILABLE", "Application name is ambiguous: " + candidates.map(\.path).sorted().prefix(8).joined(separator: ", ") + ". Use the full path")
    }
    guard candidates.count == 1, let url = candidates.first, let bundle = Bundle(url: url), let bundleID = bundle.bundleIdentifier else {
        throw SDKDomainError("TARGET_UNAVAILABLE", "Application could not be uniquely resolved; use its bundle ID or full path")
    }
    guard !AppSafetyPolicy.isBlocked(bundleIdentifier: bundleID) else { throw SDKDomainError("PERMISSION_REQUIRED", "Application is excluded from Computer Use") }
    if cancellation.isCancelled { throw SDKDomainError("CANCELLED", "Application launch cancelled") }
    let configuration = NSWorkspace.OpenConfiguration()
    configuration.activates = false
    let completion = DispatchSemaphore(value: 0)
    let result = SDKLaunchResult()
    NSWorkspace.shared.openApplication(at: url, configuration: configuration) { _, error in
        result.error = error
        completion.signal()
    }
    let deadline = Date().addingTimeInterval(10)
    while completion.wait(timeout: .now() + 0.05) == .timedOut {
        if cancellation.isCancelled || Date() > deadline {
                throw SDKDomainError(cancellation.isCancelled ? "CANCELLED" : "TIMEOUT", "Application launch outcome is uncertain; inspect running applications before retrying", effect: "possible")
        }
    }
    if let error = result.error {
        throw SDKDomainError("TARGET_UNAVAILABLE", "Application launch failed: \(error.localizedDescription)", effect: "possible")
    }
    let launched = AppDiscovery.runningApps().filter { $0.bundleIdentifier == bundleID }
    guard launched.count == 1, let app = launched.first else {
        throw SDKDomainError("TARGET_UNAVAILABLE", "Launched application is not uniquely available", effect: "possible")
    }
    let resolved = try allowed(app)
    // LaunchServices completion can precede the app's first accessible window.
    while AXIsProcessTrusted() {
        if cancellation.isCancelled {
            throw SDKDomainError("CANCELLED", "Application launched; waiting for its window was cancelled", effect: "possible")
        }
        do {
            _ = try SnapshotBuilder.build(for: resolved, treeLimits: AccessibilityTreeLimits(maxNodeCount: 1, maxDepth: 1), recoveryPolicy: .readOnly, includeScreenshot: false)
            break
        } catch ComputerUseError.stateUnavailable(let message) where message == computerUseNoWindowFoundMessage {
            guard Date() < deadline, !app.runningApplication.isTerminated else {
                throw SDKDomainError("TARGET_UNAVAILABLE", "Application launched but no accessible window became ready; do not relaunch or use another app to bypass this", effect: "possible")
            }
            Thread.sleep(forTimeInterval: 0.05)
        } catch {
            let failure = SDKDomainError(error)
            throw SDKDomainError(failure.code, "Application launched; window readiness failed: \(failure.message)", effect: "possible")
        }
    }
    if cancellation.isCancelled {
        throw SDKDomainError("CANCELLED", "Application launch outcome is uncertain; inspect running applications before retrying", effect: "possible")
    }
    return resolved
}

private final class SDKLaunchResult: @unchecked Sendable {
    var error: Error?
}
