import AppKit
import XCTest
@testable import OpenComputerUseKit

final class AppIconTests: XCTestCase {
    func testIconQueryDoesNotOpenControlOrChangeToolResults() throws {
        let desktop = MacOSSDKDesktop()
        let result = try desktop.call("getAppIcon", params: ["identity": "com.apple.finder"], cancellation: SDKCancellation()) as! [String: Any]
        XCTAssertEqual(result["status"] as? String, "available")
        let icon = result["icon"] as! [String: String]
        XCTAssertEqual(icon["mimeType"], "image/png")
        let data = try XCTUnwrap(Data(base64Encoded: try XCTUnwrap(icon["dataBase64"])))
        let bitmap = try XCTUnwrap(NSBitmapImageRep(data: data))
        XCTAssertEqual(bitmap.pixelsWide, 64)
        XCTAssertEqual(bitmap.pixelsHigh, 64)
        XCTAssertLessThanOrEqual(icon["dataBase64"]!.count, 65536)
        XCTAssertTrue(desktop.listAppSessions().isEmpty)
        XCTAssertNil(ToolCallResult(content: [.text("state")]).asDictionary["_meta"])
    }

    func testUnknownIdentityAndInvalidInputDoNotResolveOrLaunchApps() throws {
        let desktop = MacOSSDKDesktop()
        let result = try desktop.call("getAppIcon", params: ["identity": "invalid.example.missing"], cancellation: SDKCancellation()) as! [String: String]
        XCTAssertEqual(result, ["status": "unavailable"])
        for input: [String: Any] in [[:], ["identity": ""], ["identity": "com.apple.finder", "launch": true]] {
            XCTAssertThrowsError(try desktop.call("getAppIcon", params: input, cancellation: SDKCancellation()))
        }
        XCTAssertTrue(desktop.listAppSessions().isEmpty)
    }
}
