import AppKit
import XCTest
@testable import OpenComputerUseKit

final class SDKDesktopInputTests: XCTestCase {
    func testReadOnlyTextRolesDoNotAdvertiseSetValue() {
        for role in ["AXTextField", "AXTextArea", "AXTextView", "AXComboBox"] {
            let readOnly = record(role: role, settable: false)
            XCTAssertFalse(readOnly.sdkActions.contains("setValue"), role)
            XCTAssertTrue(readOnly.sdkActions.contains("click"), "Display fields must remain clickable")
            XCTAssertTrue(record(role: role, settable: true).sdkActions.contains("setValue"), role)
        }
    }

    func testNonTextValuesAndSyntheticTextDoNotAdvertiseStringInput() {
        XCTAssertFalse(record(role: "AXCheckBox", settable: true).sdkActions.contains("setValue"))
        XCTAssertFalse(record(role: "AXTextArea", settable: true, synthetic: true).sdkActions.contains("setValue"))
    }

    func testSetValuePreflightRejectionHasNoEffectButWriteFailureStaysUncertain() {
        let rejected = SDKDomainError(ComputerUseError.unsupportedAction(nonSettableSetValueErrorMessage))
        XCTAssertEqual(rejected.code, "UNSUPPORTED_CAPABILITY")
        XCTAssertEqual(rejected.effect, "none")
        let unavailable = SDKDomainError(ComputerUseError.actionPreconditionFailed("AX query failed"))
        XCTAssertEqual(unavailable.code, "TARGET_UNAVAILABLE")
        XCTAssertEqual(unavailable.effect, "none")
        let writeFailure = SDKDomainError(ComputerUseError.message("AXUIElementSetAttributeValue failed"))
        XCTAssertEqual(writeFailure.effect, "possible")
    }

    func testAXHitTestUsesWindowPointOnceForScaledScreenshots() throws {
        let service = ComputerUseService()
        for imageSize in [CGSize(width: 100, height: 60), CGSize(width: 400, height: 240)] {
            let image = try XCTUnwrap(NSBitmapImageRep(
                bitmapDataPlanes: nil, pixelsWide: Int(imageSize.width), pixelsHigh: Int(imageSize.height),
                bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false,
                colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0
            ))
            let snapshot = AppSnapshot(
                app: RunningAppDescriptor(name: "Test", bundleIdentifier: nil, pid: getpid(), runningApplication: .current),
                windowTitle: nil, windowBounds: CGRect(x: 300, y: 150, width: 200, height: 120),
                targetWindowID: nil, targetWindowLayer: nil,
                screenshotPNGData: try XCTUnwrap(image.representation(using: .png, properties: [:])),
                mode: .accessibility, treeLines: [], focusedSummary: nil, focusedElement: nil,
                selectedText: nil, elements: [:]
            )
            // Both element centers and screenshot clicks reach the hit tester in window points.
            let screenshotPoint = CGPoint(x: imageSize.width / 4, y: imageSize.height / 2)
            let coordinatePoint = screenshotPixelToWindowPoint(screenshotPoint, screenshotPixelSize: imageSize, windowBounds: snapshot.windowBounds)
            for point in [CGPoint(x: 50, y: 60), coordinatePoint] {
                var captured: CGPoint?
                _ = try service.hitTestElement(at: point, in: snapshot) { _, x, y, _ in
                    captured = CGPoint(x: CGFloat(x), y: CGFloat(y))
                    return .noValue
                }
                XCTAssertEqual(captured, CGPoint(x: 350, y: 210))
            }
        }
    }

    private func record(role: String, settable: Bool, synthetic: Bool = false) -> ElementRecord {
        ElementRecord(index: 7, identifier: nil, element: nil,
                      localFrame: CGRect(x: 10, y: 20, width: 300, height: 40), role: role,
                      rawActions: [], prettyActions: [], isSyntheticText: synthetic, isValueSettable: settable)
    }
}
