import CoreGraphics
import XCTest
@testable import OpenComputerUseKit

// Cases adapted from Cua Driver's reveal_scroll.rs tests (MIT, trycua/cua@50d1e84).
final class RevealScrollTests: XCTestCase {
    private let viewport = CGRect(x: 0, y: 100, width: 800, height: 500)

    private func row(_ y: CGFloat) -> CGRect { CGRect(x: 10, y: y, width: 600, height: 40) }

    func testDownRevealsTheRowClosestToTheDistanceBelowTheEdge() {
        let rows = (0..<30).map { row(100 + 50 * CGFloat($0)) }
        let picked = RevealScroll.pickRevealTarget(viewport: viewport, direction: .down, distance: 360, candidates: rows)
        // Goal: a bottom edge at 960; row 16 spans 900-940, row 17 950-990.
        XCTAssertEqual(picked.map { rows[$0] }, row(900))
    }

    func testUpLeftAndRightMirrorDown() {
        let rows = (-10..<10).map { row(100 + 50 * CGFloat($0)) }
        let up = RevealScroll.pickRevealTarget(viewport: viewport, direction: .up, distance: 120, candidates: rows)
        XCTAssertEqual(up.map { rows[$0] }, row(0), "top edge closest to 100 - 120 = -20")

        let columns = (0..<20).map { CGRect(x: 100 * CGFloat($0), y: 200, width: 90, height: 40) }
        let right = RevealScroll.pickRevealTarget(viewport: viewport, direction: .right, distance: 120, candidates: columns)
        XCTAssertEqual(right.map { columns[$0].minX }, 800)
        XCTAssertNil(RevealScroll.pickRevealTarget(viewport: viewport, direction: .left, distance: 120, candidates: columns))
        let leftColumns = [CGRect(x: -100, y: 200, width: 90, height: 40), CGRect(x: -200, y: 200, width: 90, height: 40)]
        XCTAssertEqual(RevealScroll.pickRevealTarget(viewport: viewport, direction: .left, distance: 120, candidates: leftColumns), 0)
    }

    func testNothingIsPickedWithoutOffscreenRowsOrWhenOnlyContainersReachPastTheEdge() {
        XCTAssertNil(RevealScroll.pickRevealTarget(viewport: viewport, direction: .down, distance: 120, candidates: [row(120), row(300), row(550)]))
        let wrapper = CGRect(x: 0, y: 100, width: 800, height: 3000)
        XCTAssertNil(RevealScroll.pickRevealTarget(viewport: viewport, direction: .down, distance: 120, candidates: [wrapper]))
    }

    func testClampedSliversCountAsTheContentPastTheEdge() {
        // Chromium live shape: rows past the viewport bottom (y=890) as 1 pt slivers at y=889.
        var nodes = (0..<13).map { RevealScroll.Node(frame: CGRect(x: 518, y: 301 + 45 * CGFloat($0), width: 600, height: 41), isLeaf: true) }
        nodes.append(.init(frame: CGRect(x: 518, y: 300, width: 700, height: 590), isLeaf: false))
        let firstClamped = nodes.count
        nodes += Array(repeating: .init(frame: CGRect(x: 518, y: 889, width: 600, height: 1), isLeaf: true), count: 100)
        let viewport = CGRect(x: 500, y: 290, width: 800, height: 600)

        // 360 pt is half the viewport plus about 1.5 rows of 45 pt: the 2nd row past the edge.
        XCTAssertEqual(RevealScroll.pickClamped(viewport: viewport, direction: .down, distance: 360, nodes: nodes),
                       .init(index: firstClamped + 1, rows: 2, pitch: 45))
        XCTAssertEqual(RevealScroll.pickClamped(viewport: viewport, direction: .down, distance: 600, nodes: nodes)?.rows, 7)
        XCTAssertEqual(RevealScroll.pickClamped(viewport: viewport, direction: .down, distance: 1200, nodes: nodes)?.rows, 21)
        XCTAssertEqual(RevealScroll.pickClamped(viewport: viewport, direction: .down, distance: 100_000, nodes: nodes)?.index, firstClamped + 99)
        XCTAssertNil(RevealScroll.pickClamped(viewport: viewport, direction: .up, distance: 360, nodes: nodes), "no exposed slivers above; this does not prove a boundary")
        // Up counts from the nearest sliver above, the last in document order.
        var above = Array(repeating: RevealScroll.Node(frame: CGRect(x: 518, y: 290, width: 600, height: 1), isLeaf: true), count: 5)
        above += (0..<13).map { .init(frame: CGRect(x: 518, y: 301 + 45 * CGFloat($0), width: 600, height: 41), isLeaf: true) }
        XCTAssertEqual(RevealScroll.pickClamped(viewport: viewport, direction: .up, distance: 90, nodes: above)?.index, 4)
        XCTAssertEqual(RevealScroll.pickClamped(viewport: viewport, direction: .up, distance: 480, nodes: above)?.index, 0)
    }

    func testClampedSliversAreNotTreatedAsRealFramesPastTheEdge() {
        // A zero-height sliver sitting exactly on the bottom edge must not win the real-frame pick.
        let sliver = CGRect(x: 10, y: 600, width: 600, height: 0)
        XCTAssertNil(RevealScroll.pickRevealTarget(viewport: viewport, direction: .down, distance: 360, candidates: [sliver, row(300)]))
    }

    func testSelectionExcludesContentInAnotherColumn() {
        let outside = CGRect(x: 900, y: 900, width: 100, height: 40)
        let inside = row(800)
        XCTAssertEqual(RevealScroll.pickRevealTarget(viewport: viewport, direction: .down, distance: 340,
                                                    candidates: [outside, inside]), 1)
        let nodes = [
            RevealScroll.Node(frame: CGRect(x: 900, y: 599, width: 100, height: 1), isLeaf: true),
            RevealScroll.Node(frame: CGRect(x: 10, y: 800, width: 100, height: 1), isLeaf: true)
        ]
        XCTAssertNil(RevealScroll.pickClamped(viewport: viewport, direction: .down, distance: 100, nodes: nodes))
    }

    func testHorizontalSliversUseHorizontalPitchAndReverseForLeft() {
        let viewport = CGRect(x: 100, y: 0, width: 400, height: 200)
        let visible = (0..<4).map { RevealScroll.Node(frame: CGRect(x: 110 + 80 * CGFloat($0), y: 10, width: 70, height: 100), isLeaf: true) }
        let left = Array(repeating: RevealScroll.Node(frame: CGRect(x: 100, y: 10, width: 1, height: 100), isLeaf: true), count: 5)
        let right = Array(repeating: RevealScroll.Node(frame: CGRect(x: 499, y: 10, width: 1, height: 100), isLeaf: true), count: 5)
        let nodes = left + visible + right
        XCTAssertEqual(RevealScroll.pickClamped(viewport: viewport, direction: .right, distance: 400, nodes: nodes)?.index, 11)
        XCTAssertEqual(RevealScroll.pickClamped(viewport: viewport, direction: .left, distance: 400, nodes: nodes)?.index, 2)
    }

    func testClampedPitchIgnoresDuplicateLabelsAndOneLargeGap() {
        var nodes = [110, 110, 150, 190, 310].map {
            RevealScroll.Node(frame: row(CGFloat($0)), isLeaf: true)
        }
        nodes += Array(repeating: .init(frame: CGRect(x: 10, y: 599, width: 600, height: 1), isLeaf: true), count: 20)
        XCTAssertEqual(RevealScroll.pickClamped(viewport: viewport, direction: .down, distance: 500, nodes: nodes),
                       .init(index: 11, rows: 7, pitch: 40))
    }

    func testExtremeDistanceClampsBeforeIntegerConversion() {
        let nodes = [RevealScroll.Node(frame: CGRect(x: 10, y: 599, width: 600, height: 1), isLeaf: true)]
        XCTAssertEqual(RevealScroll.pickClamped(viewport: viewport, direction: .down, distance: .infinity, nodes: nodes)?.index, 0)
    }

    func testSuccessRequiresVisibleMovementInTheRequestedDirection() {
        let before = row(300)
        XCTAssertTrue(RevealScroll.moved(before: before, after: row(250), viewport: viewport, direction: .down))
        XCTAssertTrue(RevealScroll.moved(before: before, after: row(350), viewport: viewport, direction: .up))
        XCTAssertTrue(RevealScroll.moved(before: before, after: before.offsetBy(dx: -5, dy: 0), viewport: viewport, direction: .right))
        XCTAssertTrue(RevealScroll.moved(before: before, after: before.offsetBy(dx: 5, dy: 0), viewport: viewport, direction: .left))
        XCTAssertFalse(RevealScroll.moved(before: before, after: before.offsetBy(dx: -5, dy: 0), viewport: viewport, direction: .left))
        XCTAssertFalse(RevealScroll.moved(before: before, after: before, viewport: viewport, direction: .down))
        XCTAssertFalse(RevealScroll.moved(before: before, after: row(350), viewport: viewport, direction: .down))
        XCTAssertFalse(RevealScroll.moved(before: before, after: row(-100), viewport: viewport, direction: .down))
        let sliver = CGRect(x: 10, y: 599, width: 600, height: 1)
        XCTAssertTrue(RevealScroll.moved(before: sliver, after: row(350), viewport: viewport, direction: .down))
        XCTAssertFalse(RevealScroll.moved(before: sliver, after: sliver, viewport: viewport, direction: .down))
    }
}
