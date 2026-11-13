import AppKit
import ApplicationServices
import XCTest
@testable import OpenComputerUseKit

final class UpstreamNativeInputTests: XCTestCase {
    @MainActor
    func testVirtualDisplayRestoresFixtureAndPreservesExistingDisplayOrigins() async throws {
        // Match the app runtime: SDK work runs off-main while AppKit continues
        // processing display-reconfiguration notifications on the main loop.
        try await Task.detached { try UpstreamNativeInputTests.verifyVirtualDisplaySession() }.value
    }

    private static func verifyVirtualDisplaySession() throws {
        try requireLiveTests()
        guard ProcessInfo.processInfo.environment["OPEN_COMPUTER_USE_RUN_AGENT_DISPLAY_LIVE_TEST"] == "1" else {
            throw XCTSkip("Enable the controlled virtual display fixture explicitly")
        }
        guard AgentDisplay.shared.isSupported else { throw XCTSkip("Virtual display SPI unavailable") }
        let fixture = try NativeInputFixture(source: "MacOSTextInputFixture.swift", hasCommands: true)
        defer { resetOpenComputerUseBackgroundWindowState(); fixture.close() }
        try fixture.configure(["reset": true])
        let id = try XCTUnwrap(try fixture.state()["windowID"] as? UInt32)
        let app = try fixture.descriptor()
        let context = try SnapshotBuilder.resolveTargetWindow(for: app, windowID: id)
        let original = try XCTUnwrap(context.windowBounds).origin
        func displays() throws -> [CGDirectDisplayID: CGRect] {
            var ids = [CGDirectDisplayID](repeating: 0, count: 32), count: UInt32 = 0
            guard CGGetActiveDisplayList(32, &ids, &count) == .success else {
                throw SDKProtocolError("Display inventory unavailable")
            }
            return Dictionary(uniqueKeysWithValues: ids.prefix(Int(count)).map { ($0, CGDisplayBounds($0)) })
        }
        let before = try displays()
        let desktop = MacOSSDKDesktop()
        defer { try? desktop.close() }
        _ = try desktop.call("configureTask", params: ["virtualDisplay": true], cancellation: SDKCancellation())
        let discovered = try XCTUnwrap(AppDiscovery.runningApps().first { $0.pid == app.pid })
        let resolved = try XCTUnwrap(try desktop.call("resolveApp", params: ["query": discovered.sdkID,
            "launch": false], cancellation: SDKCancellation()) as? [String: Any])
        let session = try XCTUnwrap(try desktop.call("openAppSession", params: ["appId": try XCTUnwrap(resolved["id"])],
            cancellation: SDKCancellation()) as? [String: Any])
        let sessionID = try XCTUnwrap(session["id"] as? String)
        _ = try desktop.call("getAppState", params: ["appSessionId": sessionID,
            "windowId": String(id), "windowPlacement": "agentDisplay", "includeScreenshot": false], cancellation: SDKCancellation())
        let bounds = try XCTUnwrap(AgentDisplay.shared.displayBounds)
        XCTAssertEqual(AgentDisplay.shared.parkedWindowIDs(pid: app.pid), [id])
        let during = try displays()
        for (displayID, frame) in before { XCTAssertEqual(during[displayID], frame) }
        XCTAssertEqual(bounds.minX, before.values.map(\.maxX).max())
        let neighbors = before.values.filter { $0.maxX == bounds.minX }
        XCTAssertEqual(bounds.minY, try XCTUnwrap(neighbors.map(\.maxY).max()))
        let parked = try XCTUnwrap(try SnapshotBuilder.resolveTargetWindow(for: app, windowID: id).windowBounds)
        XCTAssertTrue(bounds.contains(CGPoint(x: parked.minX + 1, y: parked.minY + 1)))
        XCTAssertEqual(try desktop.prepareAppStop(["appSessionId": sessionID])().value["cleanup"] as? String, "complete")
        XCTAssertFalse(AgentDisplay.shared.isParked(windowID: id))
        XCTAssertNil(AgentDisplay.shared.displayBounds)
        // Virtual-display termination callbacks use the main queue, which the
        // production app pumps independently of SDK execution.
        let deadline = Date().addingTimeInterval(5)
        while (try displays()) != before && Date() < deadline {
            RunLoop.current.run(until: Date().addingTimeInterval(0.05))
        }
        XCTAssertEqual(try displays(), before)
        let restored = try XCTUnwrap(try SnapshotBuilder.resolveTargetWindow(for: app, windowID: id).windowBounds)
        XCTAssertTrue(AgentDisplay.isRestored(frame: restored, to: original), "expected \(original), actual \(restored.origin)")
    }

    func testExplicitBackgroundWindowDoesNotExposeAnotherDocumentsSelection() throws {
        try Self.requireLiveTests()
        let fixture = try NativeInputFixture(source: "MacOSTextInputFixture.swift", hasCommands: true)
        defer { resetOpenComputerUseBackgroundWindowState(); fixture.close() }
        try fixture.configure(["reset": true])
        let windowID = try XCTUnwrap(try fixture.state()["windowID"] as? UInt32)
        try fixture.configure(["target": "other", "text": "private other content", "selection": [0, 7]])
        let app = try fixture.descriptor()
        let snapshot = try SnapshotBuilder.build(for: app, recoveryPolicy: .readOnly,
            includeScreenshot: false, windowID: windowID)
        XCTAssertEqual(snapshot.targetWindowID, windowID)
        XCTAssertNil(snapshot.focusedElement)
        XCTAssertNil(snapshot.selectedText)
        XCTAssertNil(try SnapshotBuilder.resolveTargetWindow(for: app, windowID: windowID).focusedElement)
    }

    func testExplicitSavePanelResolvesAfterAnotherDocumentBecomesKey() throws {
        try Self.requireLiveTests()
        let fixture = try NativeInputFixture(source: "MacOSTextInputFixture.swift", hasCommands: true)
        defer { resetOpenComputerUseBackgroundWindowState(); fixture.close() }
        try fixture.configure(["reset": true, "target": "savePanel"])
        try fixture.wait { ($0["sheetWindowID"] as? Int ?? 0) > 0 }
        let id = try XCTUnwrap(try fixture.state()["sheetWindowID"] as? UInt32)
        try fixture.configure(["target": "other", "text": "other document"])
        let context = try SnapshotBuilder.resolveTargetWindow(for: fixture.descriptor(), windowID: id)
        XCTAssertEqual(context.windowID, id)
        let snapshot = try SnapshotBuilder.build(for: fixture.descriptor(), recoveryPolicy: .readOnly,
            includeScreenshot: false, windowID: id)
        XCTAssertEqual(snapshot.targetWindowID, id)
        XCTAssertTrue(snapshot.treeLines.joined().contains("recovery.txt"))
    }

    func testIndependentRecoveryRestoresFixtureAfterOwnerIsKilled() throws {
        try Self.verifyOwnerKillRecovery(virtualDisplay: false)
    }

    @MainActor
    func testRecoveryRestoresVirtualDisplayWindowBeforeRemovingDisplayAfterOwnerKill() async throws {
        guard ProcessInfo.processInfo.environment["OPEN_COMPUTER_USE_RUN_AGENT_DISPLAY_LIVE_TEST"] == "1" else {
            throw XCTSkip("Enable the controlled virtual display fixture explicitly")
        }
        try await Task.detached { try UpstreamNativeInputTests.verifyOwnerKillRecovery(virtualDisplay: true) }.value
    }

    private static func verifyOwnerKillRecovery(virtualDisplay: Bool) throws {
        try requireLiveTests()
        let fixture = try NativeInputFixture(source: "MacOSTextInputFixture.swift", hasCommands: true)
        defer { fixture.close() }
        try fixture.configure(["reset": true])
        let id = try XCTUnwrap(try fixture.state()["windowID"] as? UInt32)
        let app = try fixture.descriptor()
        let context = try SnapshotBuilder.resolveTargetWindow(for: app, windowID: id)
        let original = try XCTUnwrap(context.windowBounds).origin
        let repo = URL(fileURLWithPath: #filePath).deletingLastPathComponent().deletingLastPathComponent()
            .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
        let ready = fixture.root.appendingPathComponent("recovery-ready.json")
        let owner = Process()
        owner.executableURL = URL(fileURLWithPath: "/usr/bin/python3")
        owner.arguments = [repo.appendingPathComponent("scripts/tests/window-recovery-owner.py").path,
            repo.appendingPathComponent(".build/debug/OpenComputerUse").path,
            String(app.pid), String(id), ready.path]
        if virtualDisplay { owner.arguments?.append("--virtual-display") }
        try owner.run()
        defer { if owner.isRunning { owner.terminate(); owner.waitUntilExit() } }
        XCTAssertNotNil(waitUntil(timeout: 8) { FileManager.default.fileExists(atPath: ready.path) })
        let reply = try XCTUnwrap(try JSONSerialization.jsonObject(with: Data(contentsOf: ready)) as? [String: Any])
        let registration = try XCTUnwrap(reply["reply"] as? [String: Any])
        XCTAssertNil(registration["error"], "\(registration)")
        guard registration["error"] == nil else { return }
        // AX and WindowServer may round the backing-scale boundary by a point.
        XCTAssertEqual(try XCTUnwrap(registration["x"] as? Double), Double(original.x), accuracy: 1)
        XCTAssertEqual(try XCTUnwrap(registration["y"] as? Double), Double(original.y), accuracy: 1)
        var displaced = CGPoint(x: original.x + 60, y: original.y + 60)
        var displayID: CGDirectDisplayID?
        if virtualDisplay {
            let display = try XCTUnwrap(reply["display"] as? [String: Any])
            let created = try XCTUnwrap(display["displayID"] as? UInt32, "\(display)")
            displayID = created
            displaced = agentDisplayPlacement(windowSize: try XCTUnwrap(context.windowBounds).size,
                displayBounds: CGDisplayBounds(created))
        }
        let value = try XCTUnwrap(AXValueCreate(.cgPoint, &displaced))
        XCTAssertEqual(AXUIElementSetAttributeValue(context.windowElement, kAXPositionAttribute as CFString, value), .success)
        XCTAssertNotNil(waitUntil(timeout: 2) {
            guard let bounds = try? SnapshotBuilder.resolveTargetWindow(for: app, windowID: id).windowBounds else { return false }
            return AgentDisplay.isRestored(frame: bounds, to: displaced)
        })
        XCTAssertEqual(kill(owner.processIdentifier, SIGKILL), 0)
        owner.waitUntilExit()
        // Recovery retries for ten seconds, with a final bounded AX operation.
        XCTAssertNotNil(waitUntil(timeout: 15) {
            guard let bounds = try? SnapshotBuilder.resolveTargetWindow(for: app, windowID: id).windowBounds else { return false }
            return AgentDisplay.isRestored(frame: bounds, to: original)
        }, "The recovery child must restore after SIGKILL, without atexit in its owner; original=\(original), registered=\(registration), actual=\(String(describing: try? SnapshotBuilder.resolveTargetWindow(for: app, windowID: id).windowBounds))")
        if let displayID {
            XCTAssertNotNil(waitUntil(timeout: 5) {
                var ids = [CGDirectDisplayID](repeating: 0, count: 64), count: UInt32 = 0
                return CGGetOnlineDisplayList(64, &ids, &count) == .success && !ids.prefix(Int(count)).contains(displayID)
            }, "The owned virtual display must disappear after recovery")
        }
    }

    func testSDKQuerySelectsAndReplacesNativeTextWithoutScreenshot() throws {
        try Self.requireLiveTests()
        let fixture = try NativeInputFixture(source: "MacOSTextInputFixture.swift", hasCommands: true)
        defer { fixture.close() }
        try fixture.configure(["reset": true])
        let desktop = MacOSSDKDesktop()
        defer { try? desktop.close() }
        let running = try XCTUnwrap(NSRunningApplication(processIdentifier: fixture.process.processIdentifier))
        let app = RunningAppDescriptor(name: "CherryUpstreamFixture", bundleIdentifier: nil,
            pid: fixture.process.processIdentifier, runningApplication: running)
        let session = try desktop.openAppSession(appID: "text-fixture", app: app)
        let sessionID = try XCTUnwrap(session["id"] as? String)
        let result = try XCTUnwrap(try desktop.call("query", params: ["appSessionId": sessionID,
            "text": "Document input", "exact": true], cancellation: SDKCancellation()) as? [String: Any])
        let snapshot = try XCTUnwrap(result["snapshot"] as? [String: Any])
        let tree = try XCTUnwrap(snapshot["tree"] as? [String: Any])
        let matches = try XCTUnwrap(tree["elements"] as? [[String: Any]])
        XCTAssertEqual(matches.count, 1)
        let input = try XCTUnwrap(matches.first)
        let snapshotID = try XCTUnwrap(snapshot["id"])
        _ = try desktop.call("perform", params: ["type": "selectText", "appSessionId": sessionID,
            "snapshotId": snapshotID, "elementId": try XCTUnwrap(input["id"]),
            "text": "world", "prefix": "hello "], cancellation: SDKCancellation())
        _ = try desktop.call("perform", params: ["type": "typeText", "appSessionId": sessionID,
            "snapshotId": snapshotID, "text": "新🚀"], cancellation: SDKCancellation())
        try fixture.wait { $0["view"] as? String == "hello 新🚀" }
    }

    func testLiveFocusSelectionAndVerifiedWrites() throws {
        try Self.requireLiveTests()
        let fixture = try NativeInputFixture(source: "MacOSTextInputFixture.swift", hasCommands: true)
        defer { fixture.close() }
        let service = ComputerUseService()

        try fixture.configure(["reset": true, "selection": [0, 5]])
        let cached = try fixture.snapshot()
        try service.performTypeText(on: cached, text: "X")
        try fixture.wait { $0["view"] as? String == "X world" }

        try fixture.configure(["reset": true, "text": "中🙂文", "selection": [1, 2]])
        try service.performTypeText(on: cached, text: "新🚀")
        try service.performTypeText(on: cached, text: "!")
        try fixture.wait { $0["view"] as? String == "中新🚀!文" }

        try fixture.configure(["reset": true, "selectedWritable": false, "selection": [0, 5]])
        try service.performTypeText(on: cached, text: "Value")
        try service.performTypeText(on: cached, text: "!")
        try fixture.wait { $0["view"] as? String == "Value! world" }

        try fixture.configure(["reset": true, "target": "field", "text": "field", "selection": [0, 5]])
        try service.performTypeText(on: cached, text: "Y")
        try fixture.wait { $0["field"] as? String == "Y" }
        XCTAssertEqual(try fixture.state()["view"] as? String, "hello world")

        try fixture.configure(["reset": true, "target": "none"])
        XCTAssertThrowsError(try service.performTypeText(on: cached, text: "WRONG"))
        XCTAssertEqual(try fixture.state()["valueWrites"] as? Int, 0)

        try fixture.configure(["reset": true, "rejectSelectedWrite": true])
        XCTAssertThrowsError(try service.performTypeText(on: cached, text: "WRONG"))
        try fixture.wait { $0["selectedWrites"] as? Int == 1 }
        XCTAssertEqual(try fixture.state()["view"] as? String, "hello world")
        XCTAssertEqual(try fixture.state()["valueWrites"] as? Int, 0)

        for mode in ["normal", "delay", "ignore", "trim"] {
            try fixture.configure(["reset": true, "valueMode": mode])
            let snapshot = try fixture.snapshot()
            let element = try XCTUnwrap(snapshot.focusedElement)
            let record = try XCTUnwrap(snapshot.elements.values.first { $0.element.map { CFEqual($0, element) } == true })
            XCTAssertEqual(try ElementIdentity.read(element), record.identity, mode)
            if mode == "ignore" || mode == "trim" {
                XCTAssertThrowsError(try service.performSetValue(on: snapshot, elementIndex: String(record.index), value: " padded "))
            } else {
                try service.performSetValue(on: snapshot, elementIndex: String(record.index), value: " padded ")
                try fixture.wait { $0["view"] as? String == " padded " }
            }
            try fixture.wait { $0["valueWrites"] as? Int == 1 }
        }
        try fixture.configure(["reset": true])
        let snapshot = try fixture.snapshot()
        let element = try XCTUnwrap(snapshot.focusedElement)
        let record = try XCTUnwrap(snapshot.elements.values.first { $0.element.map { CFEqual($0, element) } == true })
        try service.performSetValue(on: snapshot, elementIndex: String(record.index), value: "")
        try fixture.wait { $0["view"] as? String == "" }
    }

    func testBlankAndWindowClicksDoNotPressOtherControls() throws {
        try Self.requireLiveTests()
        let fixture = try NativeInputFixture(source: "MacOSHitTestFixture.swift", hasCommands: false)
        defer { fixture.close() }
        let service = ComputerUseService()
        let snapshot = try fixture.snapshotWithCoordinateFrame()
        try service.performClick(on: snapshot, elementIndex: nil, x: 500, y: 300, clickCount: 1, mouseButton: "left")
        let window = try XCTUnwrap(snapshot.elements.values.first { $0.role == "AXWindow" })
        try service.performClick(on: snapshot, elementIndex: String(window.index), x: nil, y: nil, clickCount: 1, mouseButton: "left")
        Thread.sleep(forTimeInterval: 0.15)
        XCTAssertEqual(try fixture.state()["closeAttempts"] as? Int, 0)
        XCTAssertEqual(try fixture.state()["wrongClicks"] as? Int, 0)

        let point = try XCTUnwrap(try fixture.state()["buttonPoint"] as? [Double])
        try service.performClick(on: snapshot, elementIndex: nil, x: point[0], y: point[1], clickCount: 1, mouseButton: "left")
        try fixture.wait { $0["wrongClicks"] as? Int == 1 }
        XCTAssertEqual(try fixture.state()["closeAttempts"] as? Int, 0)
    }

    private static func requireLiveTests() throws {
        guard ProcessInfo.processInfo.environment["OPEN_COMPUTER_USE_RUN_UPSTREAM_LIVE_TESTS"] == "1" else {
            throw XCTSkip("Set OPEN_COMPUTER_USE_RUN_UPSTREAM_LIVE_TESTS=1 to run isolated AppKit fixtures")
        }
        guard AXIsProcessTrusted() else { throw XCTSkip("Accessibility permission is required") }
        guard ProcessInfo.processInfo.environment["OPEN_COMPUTER_USE_VISUAL_CURSOR"] == "0" else {
            throw XCTSkip("Set OPEN_COMPUTER_USE_VISUAL_CURSOR=0 for isolated input tests")
        }
    }
}

private final class NativeInputFixture {
    let process = Process()
    let root: URL
    let stateURL: URL
    let commandURL: URL
    private var serial = 0

    init(source: String, hasCommands: Bool) throws {
        root = FileManager.default.temporaryDirectory.appendingPathComponent("cherry-upstream-fixture-\(UUID().uuidString)")
        stateURL = root.appendingPathComponent("state.json")
        commandURL = root.appendingPathComponent("command.json")
        try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
        do {
            let repo = URL(fileURLWithPath: #filePath).deletingLastPathComponent().deletingLastPathComponent()
                .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
            let binary = root.appendingPathComponent("CherryUpstreamFixture")
            let compiler = Process()
            compiler.executableURL = URL(fileURLWithPath: "/usr/bin/xcrun")
            compiler.arguments = ["swiftc", repo.appendingPathComponent("scripts/tests/fixtures/\(source)").path, "-o", binary.path]
            try compiler.run()
            compiler.waitUntilExit()
            guard compiler.terminationStatus == 0 else { throw NSError(domain: "Fixture build failed", code: 1) }
            process.executableURL = binary
            process.arguments = [stateURL.path, hasCommands ? commandURL.path : "--descendant-guards"]
            try process.run()
            try wait { !$0.isEmpty }
        } catch {
            close()
            throw error
        }
    }

    func close() {
        if process.isRunning { process.terminate(); process.waitUntilExit() }
        try? FileManager.default.removeItem(at: root)
    }

    func state() throws -> [String: Any] {
        try JSONSerialization.jsonObject(with: Data(contentsOf: stateURL)) as? [String: Any] ?? [:]
    }

    func wait(_ predicate: ([String: Any]) -> Bool) throws {
        let deadline = ProcessInfo.processInfo.systemUptime + 5
        while ProcessInfo.processInfo.systemUptime < deadline {
            if let state = try? state(), predicate(state) { return }
            Thread.sleep(forTimeInterval: 0.03)
        }
        throw NSError(domain: "Fixture state timeout", code: 1)
    }

    func configure(_ values: [String: Any]) throws {
        serial += 1
        var command = values
        command["serial"] = serial
        try JSONSerialization.data(withJSONObject: command).write(to: commandURL, options: .atomic)
        try wait { $0["serial"] as? Int == serial }
    }

    func snapshot() throws -> AppSnapshot {
        return try SnapshotBuilder.build(
            for: descriptor(),
            recoveryPolicy: .readOnly, includeScreenshot: false
        )
    }

    func descriptor() throws -> RunningAppDescriptor {
        let app = try XCTUnwrap(NSRunningApplication(processIdentifier: process.processIdentifier))
        return RunningAppDescriptor(name: "CherryUpstreamFixture", bundleIdentifier: nil,
            pid: process.processIdentifier, runningApplication: app)
    }

    // These tests exercise native hit testing without screen-recording access.
    // Supply a synthetic 1:1 image frame, not evidence of a captured screenshot.
    func snapshotWithCoordinateFrame() throws -> AppSnapshot {
        let state = try snapshot()
        let bounds = try XCTUnwrap(state.windowBounds)
        let image = try XCTUnwrap(NSBitmapImageRep(bitmapDataPlanes: nil,
            pixelsWide: Int(bounds.width), pixelsHigh: Int(bounds.height), bitsPerSample: 8,
            samplesPerPixel: 4, hasAlpha: true, isPlanar: false, colorSpaceName: .deviceRGB,
            bytesPerRow: 0, bitsPerPixel: 0))
        return AppSnapshot(app: state.app, windowTitle: state.windowTitle, windowBounds: bounds,
            targetWindowID: state.targetWindowID, targetWindowLayer: state.targetWindowLayer,
            screenshotData: try XCTUnwrap(image.representation(using: .png, properties: [:])),
            mode: state.mode, treeLines: state.treeLines, focusedSummary: state.focusedSummary,
            focusedElement: state.focusedElement, selectedText: state.selectedText,
            windowElement: state.windowElement, elements: state.elements)
    }
}
