import AppKit
import XCTest
@testable import OpenComputerUseKit

final class SDKFeatureTests: XCTestCase {
    func testObservationOptionsRejectEmptyChannelsAndMalformedCaptureBeforeExecution() throws {
        for params: [String: Any] in [
            ["appSessionId": "a", "includeAX": false, "includeScreenshot": false],
            ["appSessionId": "a", "includeAX": 0],
            ["appSessionId": "a", "includeScreenshot": false, "screenshot": ["format": "jpeg"]],
            ["appSessionId": "a", "screenshot": ["jpegQuality": 1.1]],
            ["appSessionId": "a", "screenshot": ["maxDimension": 0]],
            ["appSessionId": "a", "windowId": "-1"]
        ] {
            XCTAssertThrowsError(try SDKObservationOptions(params))
        }
        let defaults = try SDKObservationOptions(["appSessionId": "a"])
        XCTAssertEqual(defaults.image, .defaults)
        let options = try SDKObservationOptions(["appSessionId": "a", "includeAX": false,
            "screenshot": ["format": "jpeg", "jpegQuality": 0.6, "maxDimension": 640], "windowId": "42"])
        XCTAssertFalse(options.observation.includeAX)
        XCTAssertEqual(options.image.format, "jpg")
        XCTAssertEqual(options.image.maxDimension, 640)
        XCTAssertEqual(options.windowID, 42)
    }

    func testQueryBudgetsAndOwnershipAreEnforcedThroughSDK() throws {
        let oldNamespace = getenv("OPEN_COMPUTER_USE_FIXTURE_NAMESPACE").map { String(cString: $0) }
        setenv("OPEN_COMPUTER_USE_FIXTURE_NAMESPACE", UUID().uuidString, 1)
        defer {
            try? FileManager.default.removeItem(at: FixtureBridge.stateFileURL.deletingLastPathComponent())
            if let oldNamespace { setenv("OPEN_COMPUTER_USE_FIXTURE_NAMESPACE", oldNamespace, 1) }
            else { unsetenv("OPEN_COMPUTER_USE_FIXTURE_NAMESPACE") }
        }
        let rect = FixtureRect(rect: CGRect(x: 0, y: 0, width: 100, height: 100))
        try FixtureBridge.writeState(FixtureAppState(windowTitle: "query", windowBounds: rect, focusedIdentifier: nil, elements: [
            .init(identifier: "window", index: 0, role: "window", title: "query", value: nil, actions: ["Raise"], frame: rect),
            .init(identifier: "a", index: 1, role: "button", title: "Send", value: nil, actions: [], frame: rect),
            .init(identifier: "b", index: 2, role: "button", title: "Send later", value: nil, actions: [], frame: rect)
        ]))
        let desktop = MacOSSDKDesktop()
        defer { try? desktop.close() }
        let app = RunningAppDescriptor(name: FixtureBridge.appName, bundleIdentifier: nil, pid: getpid(), runningApplication: .current)
        let control = try desktop.openAppSession(appID: "fixture", app: app)
        let id = try XCTUnwrap(control["id"] as? String)
        func query(_ params: [String: Any]) throws -> [String: Any] {
            let result = try desktop.call("query", params: params.merging(["appSessionId": id]) { _, rhs in rhs }, cancellation: SDKCancellation())
            return try XCTUnwrap(result as? [String: Any])
        }
        let bounded = try query(["role": "button", "maxNodes": 1])
        XCTAssertEqual(bounded["stopReason"] as? String, "max_nodes")
        XCTAssertEqual(bounded["truncated"] as? Bool, true)
        let exact = try query(["text": "Send", "exact": true])
        let snapshot = try XCTUnwrap(exact["snapshot"] as? [String: Any])
        XCTAssertEqual(exact["stopReason"] as? String, "complete")
        let tree = try XCTUnwrap(snapshot["tree"] as? [String: Any])
        let matches = try XCTUnwrap(tree["elements"] as? [[String: Any]])
        XCTAssertEqual(matches.count, 1)
        XCTAssertEqual(matches.first?["name"] as? String, "Send")
        XCTAssertEqual((snapshot["screenshot"] as? [String: Any])?["status"] as? String, "unavailable")
        _ = try query(["role": "window"])
        XCTAssertThrowsError(try desktop.call("perform", params: ["type": "click", "appSessionId": id,
            "snapshotId": try XCTUnwrap(snapshot["id"]), "elementId": "0"], cancellation: SDKCancellation())) {
            XCTAssertEqual(($0 as? SDKDomainError)?.code, "STALE_SNAPSHOT")
        }
        _ = try desktop.prepareAppStop(["appSessionId": id])()
        XCTAssertThrowsError(try query(["text": "Send"])) {
            XCTAssertEqual(($0 as? SDKDomainError)?.code, "APP_SESSION_STOPPED")
        }
    }

    func testQueryRejectsUnboundedOrEmptyCriteria() {
        for params: [String: Any] in [[:], ["text": " "], ["role": "button", "limit": 101], ["text": "Send", "maxNodes": 5001], ["text": "Send", "exact": 1]] {
            XCTAssertThrowsError(try SDKQueryOptions(params.merging(["appSessionId": "a"]) { _, rhs in rhs }))
        }
    }

    func testGUIRequestsRequireTaskProtectionButInventoryDoesNotAcquireIt() throws {
        let protection = TestTaskProtection()
        let desktop = MacOSSDKDesktop(protection: protection)
        defer { try? desktop.close() }
        _ = try desktop.call("listApps", params: [:], cancellation: SDKCancellation())
        XCTAssertEqual(protection.entries, 0)
        for method in ["resolveApp", "getAppState", "query", "act", "perform"] {
            XCTAssertThrowsError(try desktop.call(method, params: [:], cancellation: SDKCancellation())) {
                XCTAssertEqual(($0 as? SDKDomainError)?.code, "PERMISSION_REQUIRED")
            }
        }
        XCTAssertEqual(protection.entries, 5)
    }

    func testStoppingAnAppKeepsTaskProtectionAndOtherAppsUntilTaskClose() throws {
        let protection = TestTaskProtection()
        let desktop = MacOSSDKDesktop(protection: protection)
        _ = try desktop.call("configureTask", params: ["lockedUse": true], cancellation: SDKCancellation())
        XCTAssertTrue(protection.enabled)
        let app = RunningAppDescriptor(name: "Fixture", bundleIdentifier: nil, pid: getpid(), runningApplication: .current)
        let first = try desktop.openAppSession(appID: "one", app: app)
        let second = try desktop.openAppSession(appID: "two", app: app)
        _ = try desktop.prepareAppStop(["appSessionId": try XCTUnwrap(first["id"])])()
        XCTAssertEqual(protection.closes, 0)
        let secondID = try XCTUnwrap(second["id"] as? String)
        XCTAssertEqual(desktop.listAppSessions().first { $0["id"] as? String == secondID }?["status"] as? String, "active")
        protection.closeFails = true
        XCTAssertThrowsError(try desktop.close()) {
            XCTAssertEqual(($0 as? SDKDomainError)?.code, "CLEANUP_FAILED")
            XCTAssertEqual(($0 as? SDKDomainError)?.effect, "possible")
        }
        XCTAssertFalse(desktop.listAppSessions().isEmpty, "Unconfirmed cleanup must retain resource ownership for retry")
        protection.closeFails = false
        try desktop.close()
        XCTAssertTrue(desktop.listAppSessions().isEmpty)
        XCTAssertEqual(protection.closes, 2)
    }

    func testSelectTextRejectsMalformedSelectionBeforeExecution() {
        let desktop = MacOSSDKDesktop()
        defer { try? desktop.close() }
        for selection: Any in [false, 1, "unknown"] {
            XCTAssertThrowsError(try desktop.call("perform", params: ["type": "selectText", "appSessionId": "a", "snapshotId": "s", "elementId": "1", "text": "target", "selection": selection], cancellation: SDKCancellation())) {
                XCTAssertEqual(($0 as? SDKDomainError)?.code, "INVALID_ARGUMENT")
            }
        }
    }
}

private final class TestTaskProtection: SDKTaskProtection, @unchecked Sendable {
    var enabled = false
    var entries = 0
    var closes = 0
    var closeFails = false
    func availability() -> [String: Any] { ["status": "available"] }
    func configure(enabled: Bool) throws { self.enabled = enabled }
    func perform(cancellation: SDKCancellation, _ body: () throws -> Any) throws -> Any {
        entries += 1
        throw SDKDomainError("PERMISSION_REQUIRED", "The task lease was not granted")
    }
    func close() throws {
        closes += 1
        if closeFails { throw SDKDomainError("CLEANUP_FAILED", "Guards have not confirmed release", effect: "possible") }
    }
}
