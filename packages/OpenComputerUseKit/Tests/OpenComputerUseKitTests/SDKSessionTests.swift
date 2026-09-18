import AppKit
import Darwin
import Foundation
import XCTest
@testable import OpenComputerUseKit

private final class SessionHarness: @unchecked Sendable {
    let incoming = Pipe()
    let outgoing = Pipe()
    let client: SDKTransport
    let server: SDKTransport
    let done = DispatchSemaphore(value: 0)
    let lock = NSLock()
    var error: Error?

    init(permissions: @escaping @Sendable (SDKCancellation) throws -> [[String: String]] = { _ in [] }, permissionRequest: (@Sendable ([String], SDKCancellation) throws -> [[String: String]])? = nil, cleanup: @escaping @Sendable () throws -> Void = {}, desktop: (any SDKDesktopBackend)? = nil) throws {
        signal(SIGPIPE, SIG_IGN)
        server = try SDKTransport(input: incoming.fileHandleForReading, output: outgoing.fileHandleForWriting)
        client = try SDKTransport(input: outgoing.fileHandleForReading, output: incoming.fileHandleForWriting)
        let session = SDKSession(sessionID: "owned", version: "test", transport: server, permissions: permissions, permissionRequest: permissionRequest, cleanup: cleanup, desktop: desktop)
        DispatchQueue.global().async {
            do { try session.run() }
            catch { self.lock.withLock { self.error = error } }
            try? self.outgoing.fileHandleForWriting.close()
            self.done.signal()
        }
    }

    func send(_ id: String?, _ method: String, _ params: [String: Any] = [:]) throws {
        var request: [String: Any] = ["jsonrpc": "2.0", "method": method, "params": params]
        if let id { request["id"] = id }
        try client.write(SDKMessage(request))
    }

    func receive() throws -> [String: Any] {
        // Bound failed assertions instead of hanging the entire Swift test process.
        let timeout = DispatchWorkItem { self.client.stop() }
        DispatchQueue.global().asyncAfter(deadline: .now() + 3, execute: timeout)
        defer { timeout.cancel() }
        return try XCTUnwrap(client.read()?.value)
    }

    func initialize() throws {
        try send("init", "initialize", ["protocolVersion": 2, "sessionId": "owned"])
        XCTAssertEqual((try receive()["result"] as? [String: Any])?["ownership"] as? String, "private")
    }

    func finish() throws {
        try send("close", "shutdown", ["sessionId": "owned"])
        XCTAssertEqual((try receive()["result"] as? [String: Any])?["cleanup"] as? String, "complete")
        XCTAssertEqual(done.wait(timeout: .now() + 3), .success)
        XCTAssertNil(lock.withLock { error })
    }

    deinit {
        client.stop()
        server.stop()
        try? incoming.fileHandleForWriting.close()
    }
}

private final class PermissionGate: @unchecked Sendable {
    let started = DispatchSemaphore(value: 0)
    let cancelled = DispatchSemaphore(value: 0)
    let release = DispatchSemaphore(value: 0)
    let lock = NSLock()
    var calls = 0
    var cleaned = false
    func query(_ cancellation: SDKCancellation) -> [[String: String]] {
        lock.withLock { calls += 1 }
        started.signal()
        let deadline = ProcessInfo.processInfo.systemUptime + 2
        while !cancellation.isCancelled && ProcessInfo.processInfo.systemUptime < deadline { Thread.sleep(forTimeInterval: 0.001) }
        if cancellation.isCancelled { cancelled.signal() }
        _ = release.wait(timeout: .now() + 3)
        return []
    }
}

final class SDKSessionTests: XCTestCase {
    func testPermissionRequestValidatesTheWholeListBeforePrompting() throws {
        let h = try SessionHarness()
        try h.initialize()
        let invalid: [[String: Any]] = [[:], ["ids": []], ["ids": ["accessibility", "accessibility"]],
                                       ["ids": ["accessibility", "unknown"]], ["ids": ["accessibility"], "extra": true]]
        for params in invalid {
            try h.send("permission", "requestPermissions", params)
            let response = try h.receive()
            XCTAssertEqual(errorCode(response), "INVALID_ARGUMENT")
            XCTAssertEqual(((response["error"] as? [String: Any])?["data"] as? [String: String])?["effect"], "none")
        }
        try h.finish()
    }

    func testPermissionRequestReturnsObservedStateWithoutAssumingGrant() throws {
        let gate = PermissionGate()
        let status = [["id": "accessibility", "label": "Accessibility", "status": "unknown", "interaction": "systemSettings"]]
        let h = try SessionHarness(permissions: { _ in status }, permissionRequest: { ids, _ in
            XCTAssertEqual(ids, ["accessibility"])
            gate.lock.withLock { gate.calls += 1 }
            return status
        })
        try h.initialize()
        try h.send("query", "getPermissionStatus")
        XCTAssertEqual((try h.receive()["result"] as? [String: Any])?["permissions"] as? [[String: String]], status)
        XCTAssertEqual(gate.lock.withLock { gate.calls }, 0)
        try h.send("request", "requestPermissions", ["ids": ["accessibility"]])
        XCTAssertEqual((try h.receive()["result"] as? [String: Any])?["permissions"] as? [[String: String]], status)
        XCTAssertEqual(gate.lock.withLock { gate.calls }, 1)
        try h.finish()
    }

    func testRunningPermissionCancellationWaitsForOnboardingCleanup() throws {
        let gate = PermissionGate()
        let h = try SessionHarness(permissionRequest: { _, cancellation in
            _ = gate.query(cancellation)
            gate.lock.withLock { gate.cleaned = true }
            throw SDKDomainError("CANCELLED", "Onboarding dismissed", effect: "possible")
        })
        try h.initialize()
        try h.send("request", "requestPermissions", ["ids": ["accessibility"]])
        XCTAssertEqual(gate.started.wait(timeout: .now() + 1), .success)
        try h.send(nil, "$/cancelRequest", ["id": "request"])
        XCTAssertEqual(gate.cancelled.wait(timeout: .now() + 1), .success)
        XCTAssertFalse(gate.lock.withLock { gate.cleaned })
        gate.release.signal()
        let response = try h.receive()
        XCTAssertTrue(gate.lock.withLock { gate.cleaned })
        XCTAssertEqual(errorCode(response), "CANCELLED")
        XCTAssertEqual(((response["error"] as? [String: Any])?["data"] as? [String: String])?["effect"], "possible")
        try h.finish()
    }

    func testPermissionCancellationPreservesPossibleSystemInteraction() throws {
        let h = try SessionHarness(permissionRequest: { _, _ in
            throw SDKDomainError("CANCELLED", "Cancelled after opening settings", effect: "possible")
        })
        try h.initialize()
        try h.send("request", "requestPermissions", ["ids": ["accessibility"]])
        let response = try h.receive()
        XCTAssertEqual(errorCode(response), "CANCELLED")
        XCTAssertEqual(((response["error"] as? [String: Any])?["data"] as? [String: String])?["effect"], "possible")
        try h.finish()
    }

    func testRejectsForeignOwnershipAndInvalidParameters() throws {
        let h = try SessionHarness()
        try h.send("early", "getCapabilities")
        XCTAssertEqual(errorCode(try h.receive()), "PROTOCOL_ERROR")
        try h.send("wrong", "initialize", ["protocolVersion": 2, "sessionId": "foreign"])
        XCTAssertEqual(errorCode(try h.receive()), "PROTOCOL_MISMATCH")
        try h.send("boolean-version", "initialize", ["protocolVersion": true, "sessionId": "owned"])
        XCTAssertEqual(errorCode(try h.receive()), "PROTOCOL_MISMATCH")
        try h.initialize()
        try h.send("bad-params", "getPermissionStatus", ["unexpected": true])
        XCTAssertEqual(errorCode(try h.receive()), "INVALID_ARGUMENT")
        try h.send("wrong-close", "shutdown", ["sessionId": "foreign"])
        XCTAssertEqual(errorCode(try h.receive()), "INVALID_ARGUMENT")
        try h.finish()
    }

    func testUnimplementedActionsCannotExecuteThroughLegacyMCP() throws {
        let h = try SessionHarness()
        try h.initialize()
        try h.send("action", "act", ["type": "click", "snapshotId": "old", "x": 0, "y": 0])
        let response = try h.receive()
        XCTAssertEqual(errorCode(response), "UNSUPPORTED_CAPABILITY")
        let data = (response["error"] as? [String: Any])?["data"] as? [String: String]
        XCTAssertEqual(data?["effect"], "none")
        try h.finish()
    }

    func testCancellationReachesRunningRequestAndSkipsQueuedRequest() throws {
        let gate = PermissionGate()
        let h = try SessionHarness(permissions: { gate.query($0) }, cleanup: { gate.lock.withLock { gate.cleaned = true } })
        try h.initialize()
        try h.send("running", "getPermissionStatus")
        XCTAssertEqual(gate.started.wait(timeout: .now() + 1), .success)
        try h.send("queued", "getPermissionStatus")
        try h.send(nil, "$/cancelRequest", ["id": "queued"])
        try h.send(nil, "$/cancelRequest", ["id": "running"])
        XCTAssertEqual(gate.cancelled.wait(timeout: .now() + 1), .success)
        try h.send("close", "shutdown", ["sessionId": "owned"])
        gate.release.signal()
        for id in ["running", "queued"] {
            let response = try h.receive()
            XCTAssertEqual(response["id"] as? String, id)
            XCTAssertEqual(errorCode(response), "CANCELLED")
        }
        XCTAssertEqual((try h.receive()["result"] as? [String: Any])?["cleanup"] as? String, "complete")
        XCTAssertEqual(h.done.wait(timeout: .now() + 3), .success)
        XCTAssertTrue(gate.lock.withLock { gate.cleaned })
        XCTAssertEqual(gate.lock.withLock { gate.calls }, 1)
    }

    func testEOFClosesResourcesWithoutShutdownRequest() throws {
        let gate = PermissionGate()
        let h = try SessionHarness(cleanup: { gate.lock.withLock { gate.cleaned = true } })
        try h.initialize()
        try h.incoming.fileHandleForWriting.close()
        XCTAssertEqual(h.done.wait(timeout: .now() + 3), .success)
        XCTAssertTrue(gate.lock.withLock { gate.cleaned })
        XCTAssertNil(h.lock.withLock { h.error })
    }

    func testCleanupFailureNeverAcknowledgesSuccess() throws {
        let h = try SessionHarness(cleanup: { throw SDKProtocolError("Resource not released") })
        try h.initialize()
        try h.send("close", "shutdown", ["sessionId": "owned"])
        XCTAssertNil(try h.client.read())
        XCTAssertEqual(h.done.wait(timeout: .now() + 3), .success)
        XCTAssertNotNil(h.lock.withLock { h.error })
    }

    func testUTF8AndMultipleFramesRemainSynchronized() throws {
        let h = try SessionHarness()
        try h.initialize()
        for index in 0..<100 {
            let id = "中文🙂-\(index)"
            try h.send(id, "getCapabilities")
            let response = try h.receive()
            XCTAssertEqual(response["id"] as? String, id)
            let capabilities = (response["result"] as? [String: Any])?["capabilities"] as? [[String: Any]]
            XCTAssertEqual(capabilities?.count, 9)
        }
        try h.finish()
    }

    private func errorCode(_ response: [String: Any]) -> String? {
        ((response["error"] as? [String: Any])?["data"] as? [String: String])?["code"]
    }
}

private final class ReceiptBackend: SDKDesktopBackend, @unchecked Sendable {
    let started = DispatchSemaphore(value: 0)
    let cancelled = DispatchSemaphore(value: 0)
    let release = DispatchSemaphore(value: 0)
    let unknown: Bool
    init(unknown: Bool) { self.unknown = unknown }
    func capabilities() -> [String: [String: Any]] { [:] }
    func close() throws {}
    func call(_ method: String, params: [String: Any], cancellation: SDKCancellation) throws -> Any {
        if unknown { throw SDKDomainError("TARGET_UNAVAILABLE", "Lost action reply", effect: "possible") }
        started.signal()
        let deadline = ProcessInfo.processInfo.systemUptime + 2
        while !cancellation.isCancelled && ProcessInfo.processInfo.systemUptime < deadline { Thread.sleep(forTimeInterval: 0.001) }
        if cancellation.isCancelled { cancelled.signal() }
        _ = release.wait(timeout: .now() + 2)
        return ["status": "completed", "observation": ["status": "unavailable", "reason": ["code": "CANCELLED", "message": "Observation cancelled"]]] as [String: Any]
    }
}

private final class AppSessionGate: SDKDesktopBackend, @unchecked Sendable {
    let desktop = MacOSSDKDesktop()
    let started = DispatchSemaphore(value: 0)
    let cancelled = DispatchSemaphore(value: 0)
    let release = DispatchSemaphore(value: 0)
    let appSessionID: String
    let otherID: String
    init() throws {
        let app = RunningAppDescriptor(name: "Fixture", bundleIdentifier: nil, pid: getpid(), runningApplication: .current)
        appSessionID = try desktop.openAppSession(appID: "first", app: app)["id"] as! String
        otherID = try desktop.openAppSession(appID: "second", app: app)["id"] as! String
    }
    func capabilities() -> [String: [String: Any]] { [:] }
    func close() throws { try desktop.close() }
    func listAppSessions() -> [[String: Any]] { desktop.listAppSessions() }
    func prepareAppStop(_ params: [String: Any]) throws -> @Sendable () throws -> SDKMessage { try desktop.prepareAppStop(params) }
    func call(_ method: String, params: [String: Any], cancellation: SDKCancellation) throws -> Any {
        let id = params["appSessionId"] as! String
        let control = try desktop.beginAppRequest(id, cancellation: cancellation)
        defer { desktop.endAppRequest(control) }
        if id == otherID { return ["available": true] }
        started.signal()
        let deadline = ProcessInfo.processInfo.systemUptime + 2
        while !cancellation.isCancelled && ProcessInfo.processInfo.systemUptime < deadline { Thread.sleep(forTimeInterval: 0.001) }
        if cancellation.isCancelled { cancelled.signal() }
        _ = release.wait(timeout: .now() + 2)
        throw SDKDomainError("CANCELLED", "Input settled without execution")
    }
}

extension SDKSessionTests {
    func testAppStopReachesBlockedRequestAndRejectsQueuedWorkWithoutStoppingOtherApps() throws {
        let backend = try AppSessionGate()
        let h = try SessionHarness(desktop: backend)
        try h.initialize()
        let params = ["appSessionId": backend.appSessionID]
        try h.send("running", "act", params)
        XCTAssertEqual(backend.started.wait(timeout: .now() + 1), .success)
        for index in 0..<63 { try h.send("queued-\(index)", "act", params) }
        try h.send("stop", "stopAppSession", params)
        XCTAssertEqual(backend.cancelled.wait(timeout: .now() + 1), .success)
        try h.send("state", "listAppSessions")
        let state = try h.receive()
        XCTAssertEqual(state["id"] as? String, "state", "Stop must not acknowledge before the active request settles")
        let sessions = try XCTUnwrap(state["result"] as? [[String: Any]])
        XCTAssertEqual(sessions.first { $0["id"] as? String == backend.appSessionID }?["status"] as? String, "stopping")
        backend.release.signal()
        var replies: [String: [String: Any]] = [:]
        for _ in 0..<65 {
            let response = try h.receive()
            replies[try XCTUnwrap(response["id"] as? String)] = response
        }
        XCTAssertEqual(errorCode(try XCTUnwrap(replies["running"])), "CANCELLED")
        for index in 0..<63 { XCTAssertEqual(errorCode(try XCTUnwrap(replies["queued-\(index)"])), "APP_SESSION_STOPPED") }
        XCTAssertEqual((replies["stop"]?["result"] as? [String: String])?["cleanup"], "complete")
        XCTAssertEqual(backend.started.wait(timeout: .now() + 0.02), .timedOut, "Queued input must not start")
        try h.send("other", "getAppState", ["appSessionId": backend.otherID])
        XCTAssertEqual((try h.receive()["result"] as? [String: Bool])?["available"], true)
        try h.send("old", "getAppState", params)
        XCTAssertEqual(errorCode(try h.receive()), "APP_SESSION_STOPPED")
        try h.send("repeat", "stopAppSession", params)
        XCTAssertEqual((try h.receive()["result"] as? [String: String])?["cleanup"], "complete")
        try h.finish()
    }

    func testUnconfirmedAppStopDoesNotReportStoppedOrPermitNewDesktopWork() throws {
        let backend = try AppSessionGate()
        let token = SDKCancellation()
        let control = try backend.desktop.beginAppRequest(backend.appSessionID, cancellation: token)
        defer { backend.desktop.endAppRequest(control) }
        let finish = try backend.prepareAppStop(["appSessionId": backend.appSessionID])
        XCTAssertTrue(token.isCancelled)
        XCTAssertThrowsError(try finish()) { error in
            XCTAssertEqual((error as? SDKDomainError)?.code, "CLEANUP_FAILED")
        }
        XCTAssertEqual(backend.listAppSessions().first { $0["id"] as? String == backend.appSessionID }?["status"] as? String, "stopping")
        XCTAssertThrowsError(try backend.desktop.call("listApps", params: [:], cancellation: SDKCancellation())) { error in
            XCTAssertEqual((error as? SDKDomainError)?.code, "CLOSED")
        }
    }

    func testActionReceiptWinsCancellationAfterDispatch() throws {
        let backend = ReceiptBackend(unknown: false)
        let h = try SessionHarness(desktop: backend)
        try h.initialize()
        try h.send("click", "act", ["type": "click"])
        XCTAssertEqual(backend.started.wait(timeout: .now() + 1), .success)
        try h.send(nil, "$/cancelRequest", ["id": "click"])
        XCTAssertEqual(backend.cancelled.wait(timeout: .now() + 1), .success)
        backend.release.signal()
        let response = try h.receive()
        XCTAssertNil(response["error"])
        XCTAssertEqual((response["result"] as? [String: Any])?["status"] as? String, "completed")
        try h.finish()
    }

    func testUncertainPlatformErrorPreservesItsEffect() throws {
        let h = try SessionHarness(desktop: ReceiptBackend(unknown: true))
        try h.initialize()
        try h.send("click", "act", ["type": "click"])
        let response = try h.receive()
        let error = response["error"] as? [String: Any]
        XCTAssertEqual((error?["data"] as? [String: String])?["effect"], "possible")
        XCTAssertEqual((error?["data"] as? [String: String])?["code"], "TARGET_UNAVAILABLE")
        try h.finish()
    }

    func testNativeDesktopRejectsForeignIDsAndMalformedOptionsBeforePlatformAccess() throws {
        let desktop = MacOSSDKDesktop()
        for options: [String: Any] in [
            ["appSessionId": "foreign", "maxTreeNodes": true],
            ["appSessionId": "foreign", "textLimit": NSNull()],
            ["appSessionId": "foreign", "activation": "force"],
        ] {
            XCTAssertThrowsError(try desktop.call("getAppState", params: options, cancellation: SDKCancellation())) { error in
                XCTAssertEqual((error as? SDKDomainError)?.code, "INVALID_ARGUMENT")
            }
        }
        XCTAssertThrowsError(try desktop.call("getAppState", params: ["appSessionId": "foreign"], cancellation: SDKCancellation())) { error in
            XCTAssertEqual((error as? SDKDomainError)?.code, "APP_SESSION_NOT_FOUND")
        }
        XCTAssertThrowsError(try desktop.call("act", params: ["type": "click", "appSessionId": "foreign", "snapshotId": "foreign", "elementId": "button"], cancellation: SDKCancellation())) { error in
            XCTAssertEqual((error as? SDKDomainError)?.code, "APP_SESSION_NOT_FOUND")
            XCTAssertEqual((error as? SDKDomainError)?.effect, "none")
        }
    }
}
