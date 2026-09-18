import AppKit
import Darwin
import Foundation
import OpenComputerUseKit

enum MacOSSDKRuntime {
    static let agentCommand = "__computer-use-sdk-agent"

    @MainActor
    static func run(arguments: [String]) throws -> Bool {
        guard arguments.first == "serve" || arguments.first == agentCommand else { return false }
        signal(SIGPIPE, SIG_IGN)
        if arguments.first == agentCommand {
            guard arguments.count == 4, let ownerPID = Int32(arguments[3]), ownerPID > 0 else {
                throw SDKProtocolError("Invalid SDK agent arguments")
            }
            let socketPath = arguments[1]
            let sessionID = arguments[2]
            _ = NSApplication.shared.setActivationPolicy(.accessory)
            DispatchQueue.global().async {
                var code: Int32 = EXIT_SUCCESS
                var connectedToOwner = false
                do {
                    let socket = try SDKSocket.connect(path: socketPath)
                    defer { try? socket.close() }
                    guard try SDKSocket.peerPID(socket.fileDescriptor) == ownerPID else {
                        throw SDKProtocolError("SDK owner identity does not match")
                    }
                    connectedToOwner = true
                    let transport = try SDKTransport(input: socket, output: socket)
                    try transport.write(SDKMessage(["sessionId": sessionID, "pid": getpid()]))
                    try SDKSession(sessionID: sessionID, version: resolvedOpenComputerUseVersion(), transport: transport, permissionRequest: SDKPermissionOnboarding.request, desktop: MacOSSDKDesktop()).run()
                } catch {
                    code = EXIT_FAILURE
                    FileHandle.standardError.write(Data("SDK agent failed: \(error)\n".utf8))
                }
                if connectedToOwner {
                    do { try SDKSocket.remove(path: socketPath) }
                    catch { code = EXIT_FAILURE }
                }
                exit(code)
            }
            NSApplication.shared.run()
            return true
        }

        guard arguments.count == 4, arguments[1] == "--stdio", arguments[2] == "--session-id", !arguments[3].isEmpty else {
            throw SDKProtocolError("Usage: serve --stdio --session-id <id>")
        }
        guard Bundle.main.bundleURL.pathExtension == "app" else {
            throw SDKProtocolError("The macOS SDK runtime must run from its complete app bundle")
        }
        let sessionID = arguments[3]
        let proxy = try SDKTransport(input: .standardInput, output: .standardOutput)
        let socketPath = try SDKSocket.makePath()
        let listener: FileHandle
        do { listener = try SDKSocket.listen(path: socketPath) }
        catch { try? SDKSocket.remove(path: socketPath); throw error }
        let owner = SDKOwnedAgent()
        _ = NSApplication.shared.setActivationPolicy(.accessory)

        DispatchQueue.global().async {
            do {
                while let request = try proxy.read() { try owner.enqueue(request) }
                owner.disconnect()
            } catch { owner.disconnect(failed: true) }
        }
        let configuration = NSWorkspace.OpenConfiguration()
        configuration.activates = false
        configuration.createsNewApplicationInstance = true
        configuration.arguments = [agentCommand, socketPath, sessionID, String(getpid())]
        NSWorkspace.shared.openApplication(at: Bundle.main.bundleURL, configuration: configuration) { app, _ in
            if let app { owner.didLaunch(app) }
            else { owner.launchFailed() }
        }

        DispatchQueue.global().async {
            var code: Int32 = EXIT_SUCCESS
            do {
                try relay(listener: listener, socketPath: socketPath, sessionID: sessionID, owner: owner, proxy: proxy)
            } catch {
                code = EXIT_FAILURE
                FileHandle.standardError.write(Data("SDK proxy failed: \(error)\n".utf8))
            }
            proxy.stop()
            try? listener.close()
            owner.terminateIfRunning()
            _ = owner.waitForExit(timeout: 1)
            do { try SDKSocket.remove(path: socketPath) }
            catch { code = EXIT_FAILURE }
            exit(code)
        }
        NSApplication.shared.run()
        return true
    }

    private static func relay(listener: FileHandle, socketPath: String, sessionID: String, owner: SDKOwnedAgent, proxy: SDKTransport) throws {
        let deadline = ProcessInfo.processInfo.systemUptime + 10
        var connected: FileHandle?
        while connected == nil && ProcessInfo.processInfo.systemUptime < deadline {
            if owner.disconnected {
                if owner.failed { throw SDKProtocolError("Invalid SDK input") }
                return
            }
            var descriptor = pollfd(fd: listener.fileDescriptor, events: Int16(POLLIN), revents: 0)
            if poll(&descriptor, 1, 100) > 0 {
                let fd = accept(listener.fileDescriptor, nil, nil)
                if fd >= 0 { connected = FileHandle(fileDescriptor: fd, closeOnDealloc: true) }
            }
        }
        guard let socket = connected else { throw SDKProtocolError("SDK agent startup timed out") }
        defer { try? socket.close() }
        guard let pid = owner.waitForPID(timeout: max(0, deadline - ProcessInfo.processInfo.systemUptime)),
              try SDKSocket.peerPID(socket.fileDescriptor) == pid else {
            throw SDKProtocolError("SDK agent identity does not match")
        }
        let agent = try SDKTransport(input: socket, output: socket)
        let watchdog = DispatchSource.makeTimerSource()
        watchdog.schedule(deadline: .now() + 10)
        watchdog.setEventHandler { agent.stop(); owner.terminateIfRunning() }
        watchdog.resume()
        defer { watchdog.cancel() }
        guard let hello = try agent.read(), hello.value["sessionId"] as? String == sessionID,
              (hello.value["pid"] as? NSNumber)?.int32Value == pid else {
            watchdog.cancel()
            throw SDKProtocolError("SDK agent handshake failed")
        }
        watchdog.cancel()

        DispatchQueue.global().async {
            do {
                while let request = owner.next() { try agent.write(request) }
                _ = Darwin.shutdown(socket.fileDescriptor, SHUT_WR)
                if !owner.waitForExit(timeout: 1.5) { owner.terminateIfRunning(); agent.stop() }
            } catch {
                owner.disconnect(failed: true)
                agent.stop()
                owner.terminateIfRunning()
            }
        }
        while let response = try agent.read() {
            if let result = response.value["result"] as? [String: Any], result["cleanup"] as? String == "complete" {
                guard result["sessionId"] as? String == sessionID,
                      response.value["id"] as? String == owner.shutdownID,
                      owner.waitForExit(timeout: 1) else {
                    throw SDKProtocolError("CLEANUP_FAILED: SDK agent exit not confirmed")
                }
                try SDKSocket.remove(path: socketPath)
                try proxy.write(response)
                owner.disconnect()
                return
            }
            if !owner.disconnected { try proxy.write(response) }
        }
        guard owner.disconnected && !owner.failed && owner.waitForExit(timeout: 1) else {
            throw SDKProtocolError("SDK agent exited without confirming shutdown")
        }
    }
}

private final class SDKOwnedAgent: @unchecked Sendable {
    private let condition = NSCondition()
    private var app: NSRunningApplication?
    private var exitSource: DispatchSourceProcess?
    private var exited = false
    private var closed = false
    private var inputFailed = false
    private var queue: [SDKMessage] = []
    private var shutdown: String?

    var disconnected: Bool { condition.withLock { closed } }
    var failed: Bool { condition.withLock { inputFailed } }
    var shutdownID: String? { condition.withLock { shutdown } }

    func didLaunch(_ application: NSRunningApplication) {
        let source = DispatchSource.makeProcessSource(identifier: application.processIdentifier, eventMask: .exit, queue: .global())
        source.setEventHandler { self.condition.withLock { self.exited = true; self.condition.broadcast() } }
        condition.withLock {
            app = application
            exitSource = source
            exited = application.isTerminated
            condition.broadcast()
        }
        source.resume()
        if disconnected { terminateIfRunning() }
    }

    func launchFailed() {
        condition.withLock { closed = true; inputFailed = true; exited = true; condition.broadcast() }
    }

    func waitForPID(timeout: TimeInterval) -> pid_t? {
        condition.lock()
        defer { condition.unlock() }
        let deadline = Date(timeIntervalSinceNow: timeout)
        while app == nil && !closed {
            if !condition.wait(until: deadline) { return nil }
        }
        return app?.processIdentifier
    }

    func enqueue(_ message: SDKMessage) throws {
        try condition.withLock {
            guard !closed, queue.count < 64 else { throw SDKProtocolError("SDK forwarding queue closed or full") }
            if message.value["method"] as? String == "shutdown" { shutdown = message.value["id"] as? String }
            queue.append(message)
            condition.signal()
        }
    }

    func next() -> SDKMessage? {
        condition.lock()
        defer { condition.unlock() }
        while queue.isEmpty && !closed { condition.wait() }
        if closed { return nil }
        return queue.removeFirst()
    }

    func disconnect(failed: Bool = false) {
        condition.withLock { closed = true; inputFailed = inputFailed || failed; queue.removeAll(); condition.broadcast() }
    }

    func waitForExit(timeout: TimeInterval) -> Bool {
        condition.lock()
        defer { condition.unlock() }
        let deadline = Date(timeIntervalSinceNow: timeout)
        while !exited {
            if !condition.wait(until: deadline) { return false }
        }
        return true
    }

    func terminateIfRunning() {
        let application = condition.withLock { exited ? nil : app }
        if let application { DispatchQueue.main.async { _ = application.forceTerminate() } }
    }
}

private enum SDKSocket {
    static func makePath() throws -> String {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent("cherry-cua-sdk-\(UUID().uuidString.prefix(12))")
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: false, attributes: [.posixPermissions: 0o700])
        return directory.appendingPathComponent("agent.sock").path
    }

    static func remove(path: String) throws {
        guard unlink(path) == 0 || errno == ENOENT else { throw SDKProtocolError("Cannot remove SDK socket") }
        guard rmdir(URL(fileURLWithPath: path).deletingLastPathComponent().path) == 0 || errno == ENOENT else {
            throw SDKProtocolError("Cannot remove SDK session directory")
        }
    }

    static func address(_ path: String) throws -> sockaddr_un {
        var address = sockaddr_un()
        address.sun_family = sa_family_t(AF_UNIX)
        address.sun_len = UInt8(MemoryLayout<sockaddr_un>.size)
        let bytes = Array(path.utf8) + [0]
        guard bytes.count <= MemoryLayout.size(ofValue: address.sun_path) else { throw SDKProtocolError("SDK socket path too long") }
        withUnsafeMutableBytes(of: &address.sun_path) { destination in destination.copyBytes(from: bytes) }
        return address
    }

    static func listen(path: String) throws -> FileHandle {
        let handle = try create()
        var address = try address(path)
        let result = withUnsafePointer(to: &address) { pointer in
            pointer.withMemoryRebound(to: sockaddr.self, capacity: 1) { Darwin.bind(handle.fileDescriptor, $0, socklen_t(MemoryLayout<sockaddr_un>.size)) }
        }
        guard result == 0, chmod(path, 0o600) == 0, Darwin.listen(handle.fileDescriptor, 1) == 0 else {
            try? handle.close()
            throw SDKProtocolError("Cannot listen for SDK agent")
        }
        return handle
    }

    static func connect(path: String) throws -> FileHandle {
        let handle = try create()
        var address = try address(path)
        let result = withUnsafePointer(to: &address) { pointer in
            pointer.withMemoryRebound(to: sockaddr.self, capacity: 1) { Darwin.connect(handle.fileDescriptor, $0, socklen_t(MemoryLayout<sockaddr_un>.size)) }
        }
        guard result == 0 else { try? handle.close(); throw SDKProtocolError("SDK owner is unavailable") }
        return handle
    }

    private static func create() throws -> FileHandle {
        let fd = socket(AF_UNIX, SOCK_STREAM, 0)
        guard fd >= 0 else { throw SDKProtocolError("Cannot create SDK socket") }
        return FileHandle(fileDescriptor: fd, closeOnDealloc: true)
    }

    static func peerPID(_ fd: Int32) throws -> pid_t {
        var pid: pid_t = 0
        var size = socklen_t(MemoryLayout<pid_t>.size)
        guard getsockopt(fd, SOL_LOCAL, LOCAL_PEERPID, &pid, &size) == 0 else { throw SDKProtocolError("Cannot verify SDK socket peer") }
        return pid
    }
}
