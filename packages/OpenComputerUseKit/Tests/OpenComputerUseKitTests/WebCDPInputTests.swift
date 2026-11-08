import Foundation
import XCTest
@testable import OpenComputerUseKit

final class WebCDPInputTests: XCTestCase {
    func testCommandLinePortParsing() {
        XCTAssertEqual(WebCDPInput.portFromCommandLine("Chrome --remote-debugging-port=9222 --user-data-dir=/tmp/x"), 9222)
        XCTAssertEqual(WebCDPInput.portFromCommandLine("\"/Applications/App.app/Contents/MacOS/App\" --remote-debugging-port=9333"), 9333)
        XCTAssertNil(WebCDPInput.portFromCommandLine("App --remote-debugging-pipe"))
        XCTAssertNil(WebCDPInput.portFromCommandLine("App"))
        XCTAssertNil(WebCDPInput.portFromCommandLine("App --remote-debugging-port=0"))
        XCTAssertNil(WebCDPInput.portFromCommandLine("App --remote-debugging-port=99999"))
    }

    func testUserDataDirParsing() {
        XCTAssertEqual(
            WebCDPInput.userDataDir(fromCommandLine: "App --user-data-dir=\"/Users/me/Library/Application Support/App\" --remote-debugging-port=1"),
            "/Users/me/Library/Application Support/App"
        )
        XCTAssertEqual(WebCDPInput.userDataDir(fromCommandLine: "App --user-data-dir=/tmp/profile"), "/tmp/profile")
        XCTAssertNil(WebCDPInput.userDataDir(fromCommandLine: "App"))
    }

    func testActivePortFileParsing() throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: directory) }
        let file = directory.appendingPathComponent("DevToolsActivePort")
        try "9223\n/devtools/browser/abc".write(to: file, atomically: true, encoding: .utf8)
        XCTAssertEqual(WebCDPInput.portFromActivePortFile(file.path), 9223)
        XCTAssertNil(WebCDPInput.portFromActivePortFile(directory.appendingPathComponent("missing").path))
    }

    func testTargetSelection() {
        let targets = [
            WebCDPInput.Target(title: "Other", webSocketURL: "ws://other"),
            WebCDPInput.Target(title: "Docs", webSocketURL: "ws://docs"),
        ]
        XCTAssertEqual(WebCDPInput.selectTarget(targets, windowTitle: "Docs - Profile 1 - Microsoft Edge")?.webSocketURL, "ws://docs")
        XCTAssertEqual(WebCDPInput.selectTarget(targets, windowTitle: "Docs")?.webSocketURL, "ws://docs")
        XCTAssertEqual(WebCDPInput.selectTarget(targets, windowTitle: "Unknown")?.webSocketURL, "ws://other")
        XCTAssertNil(WebCDPInput.selectTarget([], windowTitle: "Docs"))
    }

    func testKeyDescriptors() {
        XCTAssertEqual(WebCDPInput.descriptor(forKey: "Return")?.key, "Enter")
        XCTAssertEqual(WebCDPInput.descriptor(forKey: "Return")?.virtualKey, 13)
        XCTAssertEqual(WebCDPInput.descriptor(forKey: "return")?.text, "\r")
        XCTAssertEqual(WebCDPInput.descriptor(forKey: "backspace")?.virtualKey, 8)
        XCTAssertEqual(WebCDPInput.descriptor(forKey: "f12")?.virtualKey, 0x7B)

        let plus = WebCDPInput.descriptor(forKey: "+")
        XCTAssertEqual(plus?.key, "+")
        XCTAssertEqual(plus?.text, "+")

        let chord = WebCDPInput.descriptor(forKey: "ctrl+shift+s")
        XCTAssertEqual(chord?.key, "s")
        XCTAssertEqual(chord?.modifiers, 2 | 8)

        XCTAssertEqual(WebCDPInput.descriptor(forKey: "ctrl++")?.key, "+")
        XCTAssertEqual(WebCDPInput.descriptor(forKey: "ctrl++")?.modifiers, 2)
        XCTAssertNil(WebCDPInput.descriptor(forKey: "ctrl+nonsense"))
    }
}
