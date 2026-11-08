import CoreGraphics
import XCTest
@testable import OpenComputerUseKit

final class WindowCaptureGeometryTests: XCTestCase {
    private let bounds = CGRect(x: 20, y: 30, width: 400, height: 200)

    func testPixelRoundingAndInvalidPixelSizes() {
        XCTAssertTrue(WindowCaptureGeometry.matchesFullWindow(pixelSize: CGSize(width: 799, height: 400), bounds: bounds))
        XCTAssertFalse(WindowCaptureGeometry.matchesFullWindow(pixelSize: CGSize(width: 790, height: 400), bounds: bounds))
        for size in [CGSize.zero, CGSize(width: CGFloat.nan, height: 400), CGSize(width: 800, height: CGFloat.infinity)] {
            XCTAssertFalse(WindowCaptureGeometry.matchesFullWindow(pixelSize: size, bounds: bounds))
        }
    }
    func testAXGeometryPreservesZeroSizesButRejectsNonFiniteNegativeAndOverflow() {
        XCTAssertEqual(validatedAXFrame(position: CGPoint(x: 25, y: 40), size: .zero, windowBounds: bounds),
                       CGRect(x: 5, y: 10, width: 0, height: 0))
        for bad in [CGFloat.nan, .infinity, -.infinity] {
            XCTAssertNil(validatedAXFrame(position: CGPoint(x: bad, y: 0), size: CGSize(width: 10, height: 20)))
            XCTAssertNil(validatedAXFrame(position: .zero, size: CGSize(width: 10, height: bad)))
        }
        XCTAssertNil(validatedAXFrame(position: .zero, size: CGSize(width: -1, height: 20)))
        let huge = CGFloat.greatestFiniteMagnitude
        XCTAssertNil(validatedAXFrame(position: CGPoint(x: huge, y: 0), size: CGSize(width: huge, height: 20)))
        XCTAssertNil(validatedAXFrame(position: CGPoint(x: huge, y: 0), size: .zero,
                                     windowBounds: CGRect(x: -huge, y: 0, width: 10, height: 20)))
    }
    func testInvalidBoundsAndCoordinatesDoNotTrap() {
        for frame in [CGRect.zero, .null, .infinite, CGRect(x: CGFloat.nan, y: 0, width: 400, height: 200)] {
            XCTAssertFalse(WindowCaptureGeometry.isValid(frame))
            XCTAssertFalse(WindowCaptureGeometry.matchesFullWindow(pixelSize: CGSize(width: 800, height: 400), bounds: frame))
        }
        for number in [CGFloat.nan, .infinity, -.infinity, CGFloat(Int.max)] {
            XCTAssertEqual(renderedCoordinate(number), "?")
        }
        XCTAssertEqual(renderedCoordinate(-12.75), "-12")
    }
}
