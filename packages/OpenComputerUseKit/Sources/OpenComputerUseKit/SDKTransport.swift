import Darwin
import Foundation

public struct SDKProtocolError: Error, Sendable {
    public let message: String

    public init(_ message: String) { self.message = message }
}

// JSONSerialization produces immutable values; messages are never mutated after enqueueing.
public struct SDKMessage: @unchecked Sendable {
    public let value: [String: Any]
    public init(_ value: [String: Any]) { self.value = value }
}

public final class SDKTransport: @unchecked Sendable {
    private let input: FileHandle
    private let output: FileHandle
    private let state = NSLock()
    private let writer = NSLock()
    private var stopped = false
    private var readingStopped = false
    private var buffer = Data()
    private var bodyLength: Int?

    public init(input: FileHandle, output: FileHandle) throws {
        self.input = input
        self.output = output
        for fd in Set([input.fileDescriptor, output.fileDescriptor]) {
            let flags = fcntl(fd, F_GETFL)
            guard flags >= 0, fcntl(fd, F_SETFL, flags | O_NONBLOCK) == 0 else {
                throw SDKProtocolError("Cannot configure protocol stream")
            }
        }
    }

    public func stop() { state.withLock { stopped = true } }
    public func stopReading() { state.withLock { readingStopped = true } }

    public func read() throws -> SDKMessage? {
        while !state.withLock({ stopped || readingStopped }) {
            if bodyLength == nil {
                if let end = buffer.range(of: Data("\r\n\r\n".utf8)) {
                    guard end.upperBound - buffer.startIndex <= 8192,
                          let header = String(data: buffer[..<end.lowerBound], encoding: .ascii) else {
                        throw SDKProtocolError("Invalid frame header")
                    }
                    var length: Int?
                    for line in header.components(separatedBy: "\r\n") {
                        guard let colon = line.firstIndex(of: ":") else { throw SDKProtocolError("Invalid frame header") }
                        if line[..<colon].lowercased() == "content-length" {
                            let raw = line[line.index(after: colon)...].trimmingCharacters(in: .whitespaces)
                            guard length == nil, !raw.isEmpty, raw.utf8.allSatisfy({ $0 >= 48 && $0 <= 57 }),
                                  let count = Int(raw), count > 0, count <= 64 * 1024 * 1024 else {
                                throw SDKProtocolError("Invalid Content-Length")
                            }
                            length = count
                        }
                    }
                    guard let length else { throw SDKProtocolError("Missing Content-Length") }
                    bodyLength = length
                    buffer.removeSubrange(..<end.upperBound)
                } else if buffer.count > 8192 {
                    throw SDKProtocolError("Frame header too large")
                }
            }
            if let length = bodyLength, buffer.count >= length {
                let body = Data(buffer.prefix(length))
                buffer.removeFirst(length)
                bodyLength = nil
                guard let object = try JSONSerialization.jsonObject(with: body) as? [String: Any] else {
                    throw SDKProtocolError("Expected a JSON object")
                }
                return SDKMessage(object)
            }
            var descriptor = pollfd(fd: input.fileDescriptor, events: Int16(POLLIN), revents: 0)
            let ready = poll(&descriptor, 1, 100)
            if ready < 0 {
                if errno == EINTR { continue }
                throw SDKProtocolError("Protocol read failed")
            }
            if ready == 0 { continue }
            var bytes = [UInt8](repeating: 0, count: 16 * 1024)
            let count = Darwin.read(input.fileDescriptor, &bytes, bytes.count)
            if count == 0 {
                guard buffer.isEmpty && bodyLength == nil else { throw SDKProtocolError("Truncated frame") }
                return nil
            }
            if count < 0 {
                if errno == EAGAIN || errno == EINTR { continue }
                throw SDKProtocolError("Protocol read failed")
            }
            buffer.append(contentsOf: bytes.prefix(count))
        }
        return nil
    }

    public func write(_ message: SDKMessage) throws {
        try writer.withLock {
            let body = try JSONSerialization.data(withJSONObject: message.value, options: [.sortedKeys])
            var frame = Data("Content-Length: \(body.count)\r\n\r\n".utf8)
            frame.append(body)
            let deadline = ProcessInfo.processInfo.systemUptime + 1
            try frame.withUnsafeBytes { bytes in
                var offset = 0
                while offset < bytes.count {
                    guard !state.withLock({ stopped }), ProcessInfo.processInfo.systemUptime < deadline else {
                        throw SDKProtocolError("Protocol write interrupted or timed out")
                    }
                    let count = Darwin.write(output.fileDescriptor, bytes.baseAddress!.advanced(by: offset), bytes.count - offset)
                    if count > 0 { offset += count; continue }
                    if count < 0 && (errno == EAGAIN || errno == EINTR) {
                        var descriptor = pollfd(fd: output.fileDescriptor, events: Int16(POLLOUT), revents: 0)
                        _ = poll(&descriptor, 1, 50)
                        continue
                    }
                    throw SDKProtocolError("Protocol write failed")
                }
            }
        }
    }
}
