import Foundation
import XCTest
@testable import OpenComputerUseKit

final class SDKTransportTests: XCTestCase {
    func testIncompleteReplyTimesOutWithoutDroppingItsBufferedFrame() throws {
        let incoming = Pipe(), outgoing = Pipe()
        let transport = try SDKTransport(input: incoming.fileHandleForReading, output: outgoing.fileHandleForWriting)
        try incoming.fileHandleForWriting.write(contentsOf: Data("Content-Length: 2\r\n\r\n{".utf8))
        let started = ProcessInfo.processInfo.systemUptime
        XCTAssertThrowsError(try transport.read(timeout: 0.05)) { error in
            XCTAssertEqual((error as? SDKProtocolError)?.message, "Protocol read timed out")
        }
        XCTAssertLessThan(ProcessInfo.processInfo.systemUptime - started, 2)
        try incoming.fileHandleForWriting.write(contentsOf: Data("}".utf8))
        XCTAssertEqual(try transport.read(timeout: 1)?.value.count, 0)
    }
}
