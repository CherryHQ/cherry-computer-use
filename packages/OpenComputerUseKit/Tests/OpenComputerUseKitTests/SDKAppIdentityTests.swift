import AppKit
import Foundation
import XCTest
@testable import OpenComputerUseKit

final class SDKAppIdentityTests: XCTestCase {
    func testInstalledIdentityResolutionDoesNotRequireRunningAppAndRejectsChangedIdentityBeforeLaunch() throws {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        let app = root.appendingPathComponent("IdentityFixture.app")
        let contents = app.appendingPathComponent("Contents")
        try FileManager.default.createDirectory(at: contents, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: root) }
        let id = "test.cherry.identity." + UUID().uuidString
        let plist: [String: Any] = ["CFBundleIdentifier": id, "CFBundleName": "Identity Fixture", "CFBundlePackageType": "APPL"]
        try PropertyListSerialization.data(fromPropertyList: plist, format: .xml, options: 0).write(to: contents.appendingPathComponent("Info.plist"))
        let alias = root.appendingPathComponent("Alias.app")
        try FileManager.default.createSymbolicLink(at: alias, withDestinationURL: app)
        let identity = try resolveSDKAppIdentity(alias.path)
        XCTAssertEqual(identity.id, id)
        XCTAssertEqual(identity.url.path, app.resolvingSymlinksInPath().path)
        XCTAssertTrue(NSRunningApplication.runningApplications(withBundleIdentifier: id).isEmpty)
        XCTAssertThrowsError(try resolveSDKApp(app.path, launch: true, expectedIdentity: "different.identity", cancellation: SDKCancellation())) { error in
            guard let error = error as? SDKDomainError else { return XCTFail("Expected SDK domain error") }
            XCTAssertEqual(error.code, "TARGET_UNAVAILABLE")
            XCTAssertEqual(error.effect, "none")
        }
        XCTAssertTrue(NSRunningApplication.runningApplications(withBundleIdentifier: id).isEmpty)
    }
}
