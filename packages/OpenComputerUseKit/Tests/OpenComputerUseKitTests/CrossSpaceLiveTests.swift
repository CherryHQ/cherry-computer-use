import AppKit
import ApplicationServices
import CoreGraphics
import Foundation
import XCTest
@testable import OpenComputerUseKit

/// Opt-in live regression: a Chrome window parked on another Desktop can still be
/// snapshotted, clicked and typed into from the active Space without changing
/// the active Space or the frontmost app.
@MainActor
final class CrossSpaceLiveTests: XCTestCase {
    private typealias MainCID = @convention(c) () -> UInt32
    private typealias CopySpaces = @convention(c) (UInt32) -> Unmanaged<CFArray>?
    private typealias ActiveSpace = @convention(c) (UInt32) -> UInt64
    private typealias CopySpacesForWindows = @convention(c) (UInt32, Int32, CFArray) -> Unmanaged<CFArray>?

    func testWindowOnAnotherSpaceIsReadableClickableAndTypable() throws {
        try verifyCrossSpace(includeScreenshot: false)
    }

    func testWindowOnAnotherSpaceCanBeCaptured() throws {
        try verifyCrossSpace(includeScreenshot: true)
    }

    private func verifyCrossSpace(includeScreenshot: Bool) throws {
        guard ProcessInfo.processInfo.environment["OPEN_COMPUTER_USE_RUN_CROSS_SPACE_LIVE_TEST"] == "1" else {
            throw XCTSkip("Set OPEN_COMPUTER_USE_RUN_CROSS_SPACE_LIVE_TEST=1 to run the cross-Space live test")
        }
        guard AXIsProcessTrusted() else { throw XCTSkip("Accessibility permission is required") }
        if includeScreenshot && !CGPreflightScreenCaptureAccess() {
            throw XCTSkip("Screen Recording permission is required for the cross-Space screenshot test")
        }
        setvbuf(stdout, nil, _IONBF, 0)
        let spi = SkyLightSPI.shared
        guard spi.capability.isAvailable, spi.occlusionCapability.isAvailable else {
            throw XCTSkip("SkyLight SPI unavailable")
        }
        guard let sky = dlopen("/System/Library/PrivateFrameworks/SkyLight.framework/SkyLight", RTLD_LAZY),
              let cidPtr = dlsym(sky, "SLSMainConnectionID"), let copyPtr = dlsym(sky, "SLSCopyManagedDisplaySpaces"),
              let activePtr = dlsym(sky, "SLSGetActiveSpace"),
              let spacesForPtr = dlsym(sky, "SLSCopySpacesForWindows")
        else {
            throw XCTSkip("Space SPIs unavailable")
        }
        let cid = unsafeBitCast(cidPtr, to: MainCID.self)()
        let copySpaces = unsafeBitCast(copyPtr, to: CopySpaces.self)
        let activeSpace = unsafeBitCast(activePtr, to: ActiveSpace.self)
        let copySpacesForWindows = unsafeBitCast(spacesForPtr, to: CopySpacesForWindows.self)
        func spacesOfWindow(_ id: CGWindowID) -> [UInt64] {
            ((copySpacesForWindows(cid, 0x7, [NSNumber(value: id)] as CFArray)?.takeRetainedValue() as? [NSNumber]) ?? []).map(\.uint64Value)
        }
        let chromeURL = URL(fileURLWithPath: "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome")
        guard FileManager.default.isExecutableFile(atPath: chromeURL.path) else {
            throw XCTSkip("Google Chrome is not installed at the standard path")
        }
        // Third-party processes cannot move other apps' windows between Spaces on
        // macOS 26+ (SLSMoveWindowsToManagedSpace is gated on the window-management
        // bridge), so launch the target while the other Desktop is active instead.
        let homeSpace = activeSpace(cid)
        let displays = copySpaces(cid)?.takeRetainedValue() as? [[String: Any]] ?? []
        guard let display = displays.first(where: { display in
            ((display["Spaces"] as? [[String: Any]]) ?? []).contains { ($0["id64"] as? NSNumber)?.uint64Value == homeSpace }
        }), let displayIdentifier = display["Display Identifier"] as? String else {
            throw ComputerUseError.message("Cannot identify the active Desktop's display")
        }
        let originalFrontApp = NSWorkspace.shared.frontmostApplication
        let missionControl = try MissionControlTestSupport(displayIdentifier: displayIdentifier) {
            let displays = copySpaces(cid)?.takeRetainedValue() as? [[String: Any]] ?? []
            guard let display = displays.first(where: { $0["Display Identifier"] as? String == displayIdentifier }) else {
                throw ComputerUseError.message("The test display disconnected")
            }
            return display
        }
        defer {
            do {
                try missionControl.activate(homeSpace)
                if let created = missionControl.createdSpace {
                    let windows = CGWindowListCopyWindowInfo([.optionAll, .excludeDesktopElements], kCGNullWindowID) as? [[String: Any]] ?? []
                    let occupied = windows.contains { info in
                        guard (info[kCGWindowLayer as String] as? NSNumber)?.intValue == 0,
                              let id = info[kCGWindowNumber as String] as? NSNumber else { return false }
                        return spacesOfWindow(id.uint32Value) == [created]
                    }
                    guard !occupied else { throw ComputerUseError.message("Leaving the temporary Desktop because it still contains a window") }
                    try missionControl.removeCreatedDesktop()
                }
            } catch { XCTFail("Desktop cleanup failed: \(error)") }
            if let originalFrontApp { _ = originalFrontApp.activate(options: []) }
            RunLoop.current.run(until: Date().addingTimeInterval(0.5))
            XCTAssertEqual(activeSpace(cid), homeSpace, "cleanup must restore the original Desktop")
            if let originalFrontApp, !originalFrontApp.isTerminated {
                XCTAssertEqual(NSWorkspace.shared.frontmostApplication?.processIdentifier, originalFrontApp.processIdentifier,
                    "cleanup must restore the original frontmost app")
            }
        }
        let targetSpace = try missionControl.alternateDesktop(from: homeSpace)
        try missionControl.activate(targetSpace)
        guard activeSpace(cid) == targetSpace else {
            return XCTFail("test setup must switch to the second Desktop before launching a target")
        }
        let testRoot = Self.packageRoot.appendingPathComponent(".build/ocu-cross-space-\(UUID().uuidString)", isDirectory: true)
        let profileURL = testRoot.appendingPathComponent("chrome-profile", isDirectory: true)
        let pageURL = testRoot.appendingPathComponent("index.html")
        try FileManager.default.createDirectory(at: profileURL, withIntermediateDirectories: true)
        try Self.liveTestHTML.write(to: pageURL, atomically: false, encoding: .utf8)
        defer { try? FileManager.default.removeItem(at: testRoot) }
        let chrome = Process()
        chrome.executableURL = chromeURL
        chrome.arguments = [
            "--user-data-dir=\(profileURL.path)", "--no-first-run", "--no-default-browser-check",
            "--disable-background-networking", "--disable-component-update",
            "--window-position=200,200", "--window-size=600,420", "--app=\(pageURL.absoluteString)",
        ]
        chrome.standardOutput = FileHandle.nullDevice
        chrome.standardError = FileHandle.nullDevice
        try chrome.run()
        defer { stop(chrome) }
        defer { resetOpenComputerUseBackgroundWindowState() }
        let window = try waitForWindow(pid: chrome.processIdentifier, nameContaining: "ocu-space-", onScreenOnly: true)
        guard let runningChrome = NSRunningApplication(processIdentifier: window.pid) else { return XCTFail("Chrome not running") }
        let descriptor = RunningAppDescriptor(name: runningChrome.localizedName ?? "Google Chrome", bundleIdentifier: runningChrome.bundleIdentifier, pid: window.pid, runningApplication: runningChrome)
        _ = runningChrome.activate(options: [.activateAllWindows])
        RunLoop.current.run(until: Date().addingTimeInterval(1.5))
        XCTAssertEqual(spacesOfWindow(window.id), [targetSpace], "Chrome must have opened on the second Desktop")

        // Phase 1: visible snapshot (on the second Desktop) pins the window.
        let visibleSnapshot = try SnapshotBuilder.build(for: descriptor, recoveryPolicy: .readOnly,
            includeScreenshot: includeScreenshot, windowID: window.id)
        XCTAssertTrue(visibleSnapshot.treeLines.contains { $0.contains("sky probe") }, "visible snapshot must include web content")
        XCTAssertTrue(WindowOcclusionKeepAlive.shared.isPinned(windowID: window.id))

        // Back to the user's Desktop; bring the fixture to the front there.
        try missionControl.activate(homeSpace)
        XCTAssertEqual(activeSpace(cid), homeSpace)
        let fixture = try launch(executable: Self.packageRoot.appendingPathComponent(".build/debug/OpenComputerUseFixture"))
        defer { stop(fixture) }
        _ = try waitForWindow(pid: fixture.processIdentifier, nameContaining: "OpenComputerUseFixture", onScreenOnly: true)
        RunLoop.current.run(until: Date().addingTimeInterval(1.0))
        let parked = try waitForWindow(pid: window.pid, nameContaining: "ocu-space-", onScreenOnly: false)
        print("cross-space live test: screenshot=\(includeScreenshot) parked window spaces=\(spacesOfWindow(window.id)) onScreen=\(parked.onScreen) title=\(parked.name) activeSpace=\(activeSpace(cid)) home=\(homeSpace) other=\(targetSpace)")
        XCTAssertEqual(spacesOfWindow(window.id), [targetSpace], "the window must live on the other Space")
        XCTAssertFalse(parked.onScreen, "the target must actually be off the current Desktop")
        XCTAssertEqual(activeSpace(cid), homeSpace, "parking the window must not switch Spaces")
        let frontBefore = NSWorkspace.shared.frontmostApplication?.processIdentifier
        XCTAssertNotEqual(frontBefore, window.pid)
        let pointerBefore = NSEvent.mouseLocation

        // Phase 2: production paths against the parked window.
        let parkedSnapshot = try SnapshotBuilder.build(for: descriptor, recoveryPolicy: .readOnly,
            includeScreenshot: includeScreenshot, windowID: window.id)
        XCTAssertEqual(parkedSnapshot.targetWindowID, window.id, "snapshot must resolve the off-screen window")
        XCTAssertTrue(parkedSnapshot.treeLines.contains { $0.contains("sky probe") }, "parked snapshot must include web content")
        if includeScreenshot {
            let data = try XCTUnwrap(parkedSnapshot.screenshotData, "parked window must still be captured")
            let image = try XCTUnwrap(NSBitmapImageRep(data: data), "capture must be a decodable image")
            XCTAssertGreaterThan(image.pixelsWide, 0)
            XCTAssertGreaterThan(image.pixelsHigh, 0)
            var blueSamples = 0
            for y in stride(from: 0, to: image.pixelsHigh, by: max(1, image.pixelsHigh / 40)) {
                for x in stride(from: 0, to: image.pixelsWide, by: max(1, image.pixelsWide / 40)) {
                    if let color = image.colorAt(x: x, y: y)?.usingColorSpace(.sRGB),
                       color.redComponent < 0.5, color.greenComponent > 0.4,
                       color.greenComponent < 0.7, color.blueComponent > 0.8 { blueSamples += 1 }
                }
            }
            XCTAssertGreaterThan(blueSamples, 100, "capture must contain the test page's blue button, not a blank image")
            print("cross-space screenshot: \(image.pixelsWide)x\(image.pixelsHigh), \(data.count) bytes")
        } else {
            XCTAssertNil(parkedSnapshot.screenshotData)
        }
        XCTAssertEqual(activeSpace(cid), homeSpace, "snapshot must not switch Spaces")
        XCTAssertEqual(NSWorkspace.shared.frontmostApplication?.processIdentifier, frontBefore, "snapshot must preserve the frontmost app")

        let windowPoint = CGPoint(x: parked.bounds.width / 2, y: parked.bounds.height - 60)
        try SkyClickDispatcher.click(
            target: SkyClickTarget(
                screenPoint: CGPoint(x: parked.bounds.minX + windowPoint.x, y: parked.bounds.minY + windowPoint.y),
                windowPoint: windowPoint, windowBounds: parked.bounds, windowID: window.id, pid: window.pid),
            clickCount: 1, spi: spi)
        let clicked = try waitForWindow(pid: window.pid, nameContaining: "clicks=1", onScreenOnly: false)
        XCTAssertTrue(clicked.name.contains("clicks=1"), "sky_click must reach the parked window")

        let inputFrame = try XCTUnwrap(parkedSnapshot.elements.values.first { $0.role == "AXTextField" }?.localFrame)
        let inputPoint = CGPoint(x: inputFrame.midX, y: inputFrame.midY)
        try SkyClickDispatcher.click(
            target: SkyClickTarget(
                screenPoint: CGPoint(x: parked.bounds.minX + inputPoint.x, y: parked.bounds.minY + inputPoint.y),
                windowPoint: inputPoint, windowBounds: parked.bounds, windowID: window.id, pid: window.pid),
            clickCount: 1, spi: spi)
        try SkyKeyboardDispatcher.typeText(target: SkyKeyboardTarget(windowID: window.id, pid: window.pid), text: "s2", spi: spi)
        try SkyKeyboardDispatcher.pressKey(target: SkyKeyboardTarget(windowID: window.id, pid: window.pid), key: "cmd+a", spi: spi)
        try SkyKeyboardDispatcher.typeText(target: SkyKeyboardTarget(windowID: window.id, pid: window.pid), text: "Ok", spi: spi)
        let typed = try waitForWindow(pid: window.pid, nameContaining: "ocu-space-Ok|", onScreenOnly: false)
        print("cross-space live test: after typing title=\(typed.name)")
        XCTAssertTrue(typed.name.hasPrefix("ocu-space-Ok|clicks=1|"), "sky_key must type and select-all on the parked window")

        let desktop = MacOSSDKDesktop()
        defer { try? desktop.close() }
        let session = try desktop.openAppSession(appID: "cross-space-chrome", app: descriptor)
        let sessionID = try XCTUnwrap(session["id"] as? String)
        let query = try XCTUnwrap(try desktop.call("query", params: ["appSessionId": sessionID,
            "windowId": String(window.id), "role": "AXTextField"], cancellation: SDKCancellation()) as? [String: Any])
        let snapshot = try XCTUnwrap(query["snapshot"] as? [String: Any])
        let snapshotID = try XCTUnwrap(snapshot["id"] as? String)
        let tree = try XCTUnwrap(snapshot["tree"] as? [String: Any])
        let matches = try XCTUnwrap(tree["elements"] as? [[String: Any]])
        XCTAssertEqual(matches.count, 1, "SDK query must find the page's input on the other Desktop")
        let elementID = try XCTUnwrap(matches.first?["id"] as? String)
        _ = try desktop.call("perform", params: ["type": "selectText", "appSessionId": sessionID,
            "snapshotId": snapshotID, "elementId": elementID, "text": "Ok"], cancellation: SDKCancellation())
        _ = try desktop.call("perform", params: ["type": "typeText", "appSessionId": sessionID,
            "snapshotId": snapshotID, "text": "跨Space🙂", "keyMethod": "skyKey"], cancellation: SDKCancellation())
        let sdkTyped = try waitForWindow(pid: window.pid, nameContaining: "ocu-space-跨Space🙂|", onScreenOnly: false)
        XCTAssertTrue(sdkTyped.name.hasPrefix("ocu-space-跨Space🙂|clicks=1|"))
        print("cross-space SDK: query/selectText/typeText title=\(sdkTyped.name)")

        XCTAssertEqual(activeSpace(cid), homeSpace, "actions must not switch Spaces")
        XCTAssertEqual(NSWorkspace.shared.frontmostApplication?.processIdentifier, frontBefore, "actions must preserve the frontmost app")
        XCTAssertEqual(NSEvent.mouseLocation, pointerBefore, "actions must not move the pointer")
        XCTAssertEqual(spacesOfWindow(window.id), [targetSpace], "the window must stay on the other Space")
        XCTAssertEqual(try desktop.prepareAppStop(["appSessionId": sessionID])().value["cleanup"] as? String, "complete")
        XCTAssertFalse(WindowOcclusionKeepAlive.shared.isPinned(windowID: window.id))
    }

    private struct WindowRecord { let id: CGWindowID; let pid: pid_t; let bounds: CGRect; let name: String; let onScreen: Bool }

    private func waitForWindow(pid: pid_t, nameContaining marker: String, onScreenOnly: Bool) throws -> WindowRecord {
        let deadline = Date().addingTimeInterval(10)
        while Date() < deadline {
            let options: CGWindowListOption = onScreenOnly ? [.optionOnScreenOnly, .excludeDesktopElements] : [.optionAll]
            let raw = CGWindowListCopyWindowInfo(options, kCGNullWindowID) as? [[String: Any]] ?? []
            for info in raw {
                guard let number = info[kCGWindowNumber as String] as? NSNumber,
                      let owner = info[kCGWindowOwnerPID as String] as? NSNumber, owner.int32Value == pid,
                      let bounds = CGRect(dictionaryRepresentation: (info[kCGWindowBounds as String] as? NSDictionary) ?? [:]), bounds.width > 0
                else { continue }
                let app = AXUIElementCreateApplication(pid)
                guard let window = SnapshotBuilder.windowElement(for: number.uint32Value, appElement: app) else { continue }
                var rawTitle: CFTypeRef?
                guard AXUIElementCopyAttributeValue(window, kAXTitleAttribute as CFString, &rawTitle) == .success,
                      let name = rawTitle as? String, name.contains(marker) else { continue }
                return WindowRecord(id: number.uint32Value, pid: pid, bounds: bounds, name: name, onScreen: (info[kCGWindowIsOnscreen as String] as? NSNumber)?.boolValue ?? false)
            }
            RunLoop.current.run(until: Date().addingTimeInterval(0.1))
        }
        throw ComputerUseError.message("Timed out waiting for window title containing \(marker)")
    }

    private func launch(executable: URL) throws -> Process {
        let process = Process(); process.executableURL = executable
        process.standardOutput = FileHandle.nullDevice; process.standardError = FileHandle.nullDevice
        try process.run(); return process
    }

    private func stop(_ process: Process) {
        if process.isRunning {
            process.terminate()
            let deadline = Date().addingTimeInterval(5)
            while process.isRunning, Date() < deadline { RunLoop.current.run(until: Date().addingTimeInterval(0.05)) }
        }
        RunLoop.current.run(until: Date().addingTimeInterval(0.25))
    }

    private static let liveTestHTML = #"""
    <!doctype html><meta charset="utf-8"><title>ocu-space-ready</title>
    <style>html,body{margin:0;height:100%} input{width:90%;font:22px system-ui;margin:20px} button{position:absolute;left:0;right:0;bottom:0;height:120px;font:22px system-ui;background:#5b8def;color:#fff;border:0}</style>
    <p>sky probe paragraph</p><input id="t" autofocus placeholder="sky probe"><button id="b">click probe</button>
    <script>
      let clicks = 0; const input = document.querySelector('#t');
      const update = () => { document.title = `ocu-space-${input.value}|clicks=${clicks}|${document.visibilityState}`; };
      document.querySelector('#b').addEventListener('click', () => { clicks += 1; update(); });
      input.addEventListener('input', update); document.addEventListener('visibilitychange', update); update();
    </script>
    """#

    private static let packageRoot: URL = {
        var url = URL(fileURLWithPath: #filePath)
        for _ in 0..<5 { url.deleteLastPathComponent() }
        return url
    }()
}
