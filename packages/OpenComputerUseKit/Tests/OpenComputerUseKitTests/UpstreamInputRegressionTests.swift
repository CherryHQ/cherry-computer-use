import CoreGraphics
import XCTest
@testable import OpenComputerUseKit

final class UpstreamInputRegressionTests: XCTestCase {
    func testTypeTextReplacementPreservesUTF16SelectionAndCaret() throws {
        let replaced = try XCTUnwrap(typedTextReplacement(value: "中🙂文e\u{301}", selection: CFRange(location: 1, length: 2), text: "新🚀"))
        XCTAssertEqual(replaced.value, "中新🚀文e\u{301}")
        XCTAssertEqual(replaced.caret, 4)
        XCTAssertEqual(typedTextReplacement(value: "e\u{301}", selection: CFRange(location: 1, length: 1), text: "X")?.value, "eX")
        XCTAssertEqual(typedTextReplacement(value: "  ", selection: CFRange(location: 0, length: 0), text: "X")?.value, "X  ")
        XCTAssertEqual(typedTextReplacement(value: "abc", selection: CFRange(location: 1, length: 0), text: "X")?.value, "aXbc")
        XCTAssertEqual(typedTextReplacement(value: "abc", selection: CFRange(location: 3, length: 0), text: "X")?.caret, 4)
        XCTAssertEqual(typedTextReplacement(value: "abc", selection: CFRange(location: 0, length: 3), text: "X")?.value, "X")
        XCTAssertEqual(typedTextReplacement(value: "", selection: CFRange(location: 0, length: 0), text: "X")?.value, "X")
    }
    func testTypeTextReplacementRejectsInvalidSelection() {
        for range in [CFRange(location: -1, length: 0), CFRange(location: 4, length: 0),
                      CFRange(location: 2, length: 2), CFRange(location: 0, length: -1),
                      CFRange(location: 1, length: Int.max), CFRange(location: 1, length: 1)] {
            XCTAssertNil(typedTextReplacement(value: "🙂x", selection: range, text: "X"))
        }
    }

}
