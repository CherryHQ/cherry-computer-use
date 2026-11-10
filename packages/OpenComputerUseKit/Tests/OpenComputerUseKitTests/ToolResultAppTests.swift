import AppKit
import XCTest
@testable import OpenComputerUseKit

final class ToolResultAppTests: XCTestCase {
    func testIconIsEncodedAsSmallPNG() throws {
        let image = NSImage(size: NSSize(width: 256, height: 256), flipped: false) { rect in
            NSColor.blue.setFill()
            rect.fill()
            return true
        }
        let data = try XCTUnwrap(ToolResultApp.iconPNG(image))
        let bitmap = try XCTUnwrap(NSBitmapImageRep(data: data))
        XCTAssertEqual(bitmap.pixelsWide, 64)
        XCTAssertEqual(bitmap.pixelsHigh, 64)
        XCTAssertEqual(Array(data.prefix(8)), [137, 80, 78, 71, 13, 10, 26, 10])
        XCTAssertLessThan(data.base64EncodedString().count, 65536)
    }

    func testTargetMetadataSurvivesSerializationAndDragAnnotation() throws {
        let running = NSRunningApplication.current
        let app = ToolResultApp(app: RunningAppDescriptor(
            name: "Target App", bundleIdentifier: "com.example.target",
            pid: running.processIdentifier, runningApplication: running
        ))
        let result = ToolCallResult(content: [.text("Target state")], app: app)
        let annotated = appendingDragDeliveryNote(to: result, path: .appPost)
        let wire = try JSONSerialization.jsonObject(with: JSONSerialization.data(withJSONObject: annotated.asDictionary))
        let dictionary = try XCTUnwrap(wire as? [String: Any])
        let meta = try XCTUnwrap(dictionary["_meta"] as? [String: Any])
        let target = try XCTUnwrap(meta["com.cherrystudio.computer-use/app"] as? [String: Any])
        XCTAssertEqual(target["name"] as? String, "Target App")
        XCTAssertEqual(target["bundleIdentifier"] as? String, "com.example.target")
        XCTAssertEqual(annotated.primaryText, "Target state")
        XCTAssertEqual(annotated.content.count, 2)
        XCTAssertNil(ToolCallResult.text("No target").asDictionary["_meta"])
    }
}
