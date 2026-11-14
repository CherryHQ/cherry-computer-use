import AppKit
import XCTest
@testable import OpenComputerUseKit

private final class RecoveryBackend: WindowRecoveryBackend {
    var positions: [CGWindowID: CGPoint] = [1: CGPoint(x: 20, y: 30), 2: CGPoint(x: 40, y: 50)]
    var occlusion: [CGWindowID: Bool] = [1: true, 2: false]
    var failed: Set<CGWindowID> = []
    var retired: Set<CGWindowID> = []
    var forgotten: [CGWindowID] = []
    func retain(windowID: CGWindowID, pid: pid_t) throws {
        if retired.contains(windowID) { throw SDKProtocolError("Window identity changed") }
    }
    func position(windowID: CGWindowID) throws -> CGPoint { positions[windowID]! }
    func restore(windowID: CGWindowID, position: CGPoint) throws {
        if failed.contains(windowID) { throw SDKProtocolError("Window unavailable") }
        positions[windowID] = position
    }
    func setOcclusion(windowID: CGWindowID, enabled: Bool) throws -> Bool {
        let old = occlusion[windowID]!
        occlusion[windowID] = enabled
        return old
    }
    func forget(windowID: CGWindowID) { forgotten.append(windowID) }
}

final class WindowRecoveryTests: XCTestCase {
    func testAbruptOwnerLossRestoresPositionsAndPriorOcclusionIndependently() throws {
        let backend = RecoveryBackend()
        let recovery = WindowRecoveryLedger(backend: backend)
        for id: CGWindowID in [1, 2] {
            _ = try recovery.rememberPosition(id, pid: 42)
            _ = try recovery.pin(id, pid: 42)
            backend.positions[id] = CGPoint(x: 2500, y: 80)
        }
        backend.failed = [1]
        XCTAssertThrowsError(try recovery.recover())
        XCTAssertEqual(backend.positions[2], CGPoint(x: 40, y: 50))
        XCTAssertEqual(backend.occlusion, [1: true, 2: false])
        XCTAssertNotNil(recovery.entries[1]?.position)
        XCTAssertNil(recovery.entries[2])
        backend.failed = []
        try recovery.recover()
        XCTAssertEqual(backend.positions[1], CGPoint(x: 20, y: 30))
        XCTAssertTrue(recovery.entries.isEmpty)
        try recovery.recover()
        XCTAssertEqual(backend.forgotten.sorted(), [1, 2])
    }

    func testRepeatedParkingKeepsOriginalPositionAndRejectsAnotherOwner() throws {
        let backend = RecoveryBackend()
        let recovery = WindowRecoveryLedger(backend: backend)
        XCTAssertEqual(try recovery.rememberPosition(1, pid: 42), CGPoint(x: 20, y: 30))
        backend.positions[1] = CGPoint(x: 2500, y: 80)
        XCTAssertThrowsError(try recovery.rememberPosition(1, pid: 43))
        XCTAssertEqual(try recovery.rememberPosition(1, pid: 42), CGPoint(x: 20, y: 30))
        backend.retired = [1]
        XCTAssertThrowsError(try recovery.rememberPosition(1, pid: 42))
        XCTAssertThrowsError(try recovery.pin(1, pid: 42))
        XCTAssertEqual(backend.occlusion[1], true)
    }

    func testRestoringPositionDoesNotDropPendingOcclusionRecovery() throws {
        let backend = RecoveryBackend()
        let recovery = WindowRecoveryLedger(backend: backend)
        _ = try recovery.rememberPosition(1, pid: 42)
        _ = try recovery.pin(1, pid: 42)
        try recovery.restorePosition(1)
        XCTAssertNotNil(recovery.entries[1]?.occlusion)
        try recovery.releaseOcclusion(1)
        XCTAssertTrue(recovery.entries.isEmpty)
    }
}
