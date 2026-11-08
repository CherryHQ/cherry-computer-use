import AppKit
import ApplicationServices
import XCTest
@testable import OpenComputerUseKit

final class UpstreamNativeInputTests: XCTestCase {
    func testLiveFocusSelectionAndVerifiedWrites() throws {
        try requireLiveTests()
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
        try requireLiveTests()
        let fixture = try NativeInputFixture(source: "MacOSHitTestFixture.swift", hasCommands: false)
        defer { fixture.close() }
        let service = ComputerUseService()
        let snapshot = try fixture.snapshot()
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

    private func requireLiveTests() throws {
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
        let app = try XCTUnwrap(NSRunningApplication(processIdentifier: process.processIdentifier))
        return try SnapshotBuilder.build(
            for: RunningAppDescriptor(name: "CherryUpstreamFixture", bundleIdentifier: nil, pid: process.processIdentifier, runningApplication: app),
            recoveryPolicy: .readOnly, includeScreenshot: false
        )
    }
}
