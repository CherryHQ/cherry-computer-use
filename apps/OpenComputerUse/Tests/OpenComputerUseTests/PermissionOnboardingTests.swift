import AppKit
import XCTest
@testable import OpenComputerUse

final class PermissionOnboardingTests: XCTestCase {
    @MainActor
    func testWindowDismissalFinishesTheSDKSessionExactlyOnce() async {
        _ = NSApplication.shared
        var dismissals = 0
        let controller = PermissionWindowController(
            terminateOnCompletion: false,
            permissions: [.screenRecording],
            onDismiss: { dismissals += 1 }
        )
        controller.window?.performClose(nil)
        XCTAssertEqual(dismissals, 1)
        controller.close()
        XCTAssertEqual(dismissals, 1)
    }

    @MainActor
    func testDoneClosesOnboardingWithoutTerminatingTheSDKProcess() async throws {
        _ = NSApplication.shared
        var dismissed = false
        let controller = PermissionWindowController(
            terminateOnCompletion: false,
            permissions: [.screenRecording],
            onDismiss: { dismissed = true }
        )
        let view = try XCTUnwrap(controller.contentViewController?.view)
        let button = try XCTUnwrap(findDoneButton(view))
        button.performClick(nil)
        XCTAssertTrue(dismissed)
        XCTAssertFalse(controller.window?.isVisible ?? true)
    }

    @MainActor
    func testCancelledDropKeepsPanelAndAcceptedDropClosesIt() async throws {
        _ = NSApplication.shared
        let existing = Set(NSApp.windows.map(\.windowNumber))
        var completed = false
        let controller = PermissionAccessoryPanelController(onBack: {}, onDropAccepted: { completed = true })
        controller.show(for: .screenRecording, sourceFrameInScreen: nil)
        defer { controller.hide() }
        let panel = try XCTUnwrap(NSApp.windows.first { !existing.contains($0.windowNumber) && $0 is NSPanel })
        panel.orderFront(nil)
        let tile = try XCTUnwrap(descendants(try XCTUnwrap(panel.contentView)).compactMap { $0 as? DraggableAppTileView }.first)
        let session = NSDraggingSession()
        tile.draggingSession(session, endedAt: .zero, operation: [])
        XCTAssertTrue(panel.isVisible)
        XCTAssertFalse(completed)
        tile.draggingSession(session, endedAt: .zero, operation: .copy)
        XCTAssertFalse(panel.isVisible)
        XCTAssertTrue(completed)
    }

    @MainActor
    private func descendants(_ view: NSView) -> [NSView] {
        [view] + view.subviews.flatMap { descendants($0) }
    }

    @MainActor
    private func findDoneButton(_ view: NSView) -> NSButton? {
        if let button = view as? NSButton, button.title == "Done" { return button }
        return view.subviews.lazy.compactMap { self.findDoneButton($0) }.first
    }
}
