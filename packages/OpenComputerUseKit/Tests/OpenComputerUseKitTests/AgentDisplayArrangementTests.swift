import CoreGraphics
import XCTest
@testable import OpenComputerUseKit

final class AgentDisplayArrangementTests: XCTestCase {
    func testAgentDisplayGoesPastTheRightmostUserDisplayOnlyWhenTheirsMoved() {
        let builtIn = CGRect(x: 0, y: 0, width: 1512, height: 982)
        let external = CGRect(x: 1512, y: -200, width: 2560, height: 1440)
        // macOS put the agent display right of the built-in and pushed the external one past it.
        let pushed = external.offsetBy(dx: 1920, dy: 0)
        XCTAssertEqual(agentDisplayOrigin(displaysBefore: [builtIn, external], displaysNow: [builtIn, pushed]), CGPoint(x: 4072, y: -200))
        // The rightmost display is the one with the rightmost edge.
        let left = CGRect(x: -2560, y: 0, width: 2560, height: 1440)
        XCTAssertEqual(agentDisplayOrigin(displaysBefore: [left, builtIn], displaysNow: [left.offsetBy(dx: 0, dy: 100), builtIn]), CGPoint(x: 1512, y: 0))
        // Nothing moved, as with a single display: the arrangement is left alone.
        XCTAssertNil(agentDisplayOrigin(displaysBefore: [builtIn], displaysNow: [builtIn]))
        XCTAssertNil(agentDisplayOrigin(displaysBefore: [builtIn, external], displaysNow: [builtIn, external]))
        XCTAssertNil(agentDisplayOrigin(displaysBefore: [], displaysNow: []))
    }

    func testWindowsMacOSMovesOntoANewAgentDisplayGoBackWhereTheyWere() {
        let display = CGRect(x: 1512, y: 0, width: 1920, height: 1080)
        let builtIn = CGRect(x: 0, y: 0, width: 1512, height: 982)
        let before: [CGWindowID: CGRect] = [
            1: CGRect(x: 300, y: 268, width: 360, height: 232),
            2: CGRect(x: 720, y: 268, width: 360, height: 232),
            3: CGRect(x: 1400, y: 100, width: 400, height: 300),
            4: CGRect(x: 100, y: 100, width: 200, height: 200),
            5: CGRect(x: -1272, y: 268, width: 360, height: 232),
            6: CGRect(x: 1400, y: 500, width: 400, height: 300),
        ]
        let now: [CGWindowID: CGRect] = [
            1: CGRect(x: 1552, y: 40, width: 360, height: 232), // remembered on the agent display
            2: before[2]!, // stayed
            3: CGRect(x: 1552, y: 40, width: 400, height: 300), // hung past the edge, where macOS leaves a window it took off the last agent display: pulled onto the built-in
            // 4 closed meanwhile
            5: CGRect(x: 1552, y: 40, width: 360, height: 232), // read mid Space switch: no place to return to
            6: before[6]!, // reaches into the display's area but was not moved
        ]
        XCTAssertEqual(windowsMovedOntoAgentDisplay(display, userDisplays: [builtIn], before: before, now: now), [1: CGPoint(x: 300, y: 268), 3: CGPoint(x: 1112, y: 100)])
        XCTAssertEqual(windowsMovedOntoAgentDisplay(display, userDisplays: [builtIn], before: before, now: before), [:])
    }

}
