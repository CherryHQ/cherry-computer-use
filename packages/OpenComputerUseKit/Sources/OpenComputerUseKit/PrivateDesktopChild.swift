import AppKit
import Darwin
import Foundation
import OpenComputerUseVirtualDisplayShim

/// An inherited-pipe child of this executable. Closing stdin is the lifecycle
/// signal, including when the owner is killed without running cleanup code.
final class PrivateDesktopChild {
    let process = Process()
    let channel: SDKTransport
    private let input: FileHandle
    private let output: FileHandle
    private var disconnected = false

    init(command: String) throws {
        guard let current = Bundle.main.executableURL else { throw SDKProtocolError("Desktop executable unavailable") }
        // SwiftPM links this class into its test bundle beside OpenComputerUse.
        let executable = current.lastPathComponent == "OpenComputerUse" ? current
            : Bundle(for: PrivateDesktopChild.self).bundleURL.deletingLastPathComponent().appendingPathComponent("OpenComputerUse")
        guard FileManager.default.isExecutableFile(atPath: executable.path) else {
            throw SDKProtocolError("Build OpenComputerUse before using background desktop resources")
        }
        let requests = Pipe(), replies = Pipe()
        input = requests.fileHandleForWriting
        output = replies.fileHandleForReading
        for handle in [input, output] {
            let flags = fcntl(handle.fileDescriptor, F_GETFD)
            guard flags >= 0, fcntl(handle.fileDescriptor, F_SETFD, flags | FD_CLOEXEC) == 0 else {
                throw SDKProtocolError("Cannot isolate desktop resource pipes")
            }
        }
        channel = try SDKTransport(input: output, output: input)
        process.executableURL = executable
        process.arguments = [command]
        process.standardInput = requests
        process.standardOutput = replies
        process.standardError = FileHandle.standardError
        try process.run()
        try? requests.fileHandleForReading.close()
        try? replies.fileHandleForWriting.close()
    }

    func disconnect() {
        guard !disconnected else { return }
        disconnected = true
        channel.stop()
        try? input.close()
    }

    func finish(timeout: TimeInterval = 3) throws {
        disconnect()
        guard waitUntil(timeout: timeout, { !process.isRunning }) != nil, process.terminationStatus == 0 else {
            throw SDKProtocolError("Desktop resource process did not confirm exit")
        }
    }

    deinit {
        try? input.close()
        try? output.close()
    }
}

/// Some macOS versions keep a reconfigured CGVirtualDisplay online after its
/// object is released. A dedicated process gives teardown a kernel boundary.
/// The recovery worker owns this child so windows are restored before it exits.
final class RecoveryVirtualDisplay {
    static let command = "__computer-use-virtual-display"
    private var child: PrivateDesktopChild?
    private var id: CGDirectDisplayID = 0

    func create() throws -> CGDirectDisplayID {
        guard child == nil else { throw SDKProtocolError("Virtual display already owned") }
        let child = try PrivateDesktopChild(command: Self.command)
        self.child = child
        do {
            guard let reply = try child.channel.read(timeout: 3),
                  let id = reply.value["displayID"] as? UInt32, id != 0 else {
                throw SDKProtocolError("Virtual display child did not become ready")
            }
            self.id = id
            return id
        } catch {
            child.disconnect()
            throw error
        }
    }

    func destroy() throws {
        guard let child else { return }
        try child.finish(timeout: 2)
        let deadline = Date().addingTimeInterval(2)
        while id != 0 {
            // Enumeration refreshes CoreGraphics' per-process display cache;
            // CGDisplayIsOnline alone can keep returning its old value.
            var ids = [CGDirectDisplayID](repeating: 0, count: 64), count: UInt32 = 0
            guard CGGetOnlineDisplayList(64, &ids, &count) == .success else {
                throw SDKProtocolError("Display inventory unavailable during cleanup")
            }
            if !ids.prefix(Int(count)).contains(id) { break }
            guard Date() < deadline else { throw SDKProtocolError("Virtual display removal was not confirmed") }
            RunLoop.current.run(until: Date().addingTimeInterval(0.025))
        }
        self.child = nil
        id = 0
    }

    static func run() throws {
        let channel = try SDKTransport(input: .standardInput, output: .standardOutput)
        let displaysBefore = AgentDisplay.activeDisplays().map { (id: $0, bounds: CGDisplayBounds($0)) }
        var handle: UnsafeMutableRawPointer?
        let id = ocu_virtual_display_create("Cherry Computer Use", UInt32(AgentDisplay.displaySize.width),
            UInt32(AgentDisplay.displaySize.height), 60, &handle)
        guard id != 0, let handle else { throw SDKProtocolError("Could not create the agent display") }
        defer { ocu_virtual_display_destroy(handle) }
        // Keep the display-configuration transaction in its owning process too:
        // WindowServer can retain references for the configuring connection.
        try AgentDisplay.placeBesideUserDisplays(id, displaysBefore: displaysBefore)
        try channel.write(SDKMessage(["displayID": id]))
        // No commands or shared endpoint: EOF releases the object, then process
        // exit removes any WindowServer state retained after reconfiguration.
        if try channel.read() != nil { throw SDKProtocolError("Unexpected display ownership message") }
    }
}
