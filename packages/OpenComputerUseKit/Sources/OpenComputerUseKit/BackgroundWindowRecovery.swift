import AppKit
import ApplicationServices
import Darwin
import Foundation

/// The recovery process holds AX references independently of the automation
/// process. A closed window or reused PID must never select a new target.
protocol WindowRecoveryBackend: AnyObject {
    func retain(windowID: CGWindowID, pid: pid_t) throws
    func position(windowID: CGWindowID) throws -> CGPoint
    func restore(windowID: CGWindowID, position: CGPoint) throws
    func setOcclusion(windowID: CGWindowID, enabled: Bool) throws -> Bool
    func forget(windowID: CGWindowID)
}

final class WindowRecoveryLedger {
    struct Entry {
        let pid: pid_t
        var position: CGPoint?
        var occlusion: Bool?
    }
    private let backend: any WindowRecoveryBackend
    private(set) var entries: [CGWindowID: Entry] = [:]

    init(backend: any WindowRecoveryBackend) { self.backend = backend }

    private func retain(_ id: CGWindowID, pid: pid_t) throws {
        if let entry = entries[id] {
            guard entry.pid == pid else { throw SDKProtocolError("Recovery window owner changed") }
        }
        try backend.retain(windowID: id, pid: pid)
        if entries[id] == nil {
            entries[id] = Entry(pid: pid)
        }
    }

    func rememberPosition(_ id: CGWindowID, pid: pid_t) throws -> CGPoint {
        try retain(id, pid: pid)
        if let original = entries[id]?.position { return original }
        do {
            let original = try backend.position(windowID: id)
            entries[id]?.position = original
            return original
        } catch { discardIfIdle(id); throw error }
    }

    func pin(_ id: CGWindowID, pid: pid_t) throws -> Bool {
        try retain(id, pid: pid)
        if let original = entries[id]?.occlusion { return original }
        do {
            let original = try backend.setOcclusion(windowID: id, enabled: false)
            entries[id]?.occlusion = original
            return original
        } catch { discardIfIdle(id); throw error }
    }

    func restorePosition(_ id: CGWindowID) throws {
        if let position = entries[id]?.position {
            try backend.restore(windowID: id, position: position)
            entries[id]?.position = nil
        }
        discardIfIdle(id)
    }

    func releaseOcclusion(_ id: CGWindowID) throws {
        if let original = entries[id]?.occlusion {
            _ = try backend.setOcclusion(windowID: id, enabled: original)
            entries[id]?.occlusion = nil
        }
        discardIfIdle(id)
    }

    private func discardIfIdle(_ id: CGWindowID) {
        if let entry = entries[id], entry.position == nil && entry.occlusion == nil {
            entries.removeValue(forKey: id)
            backend.forget(windowID: id)
        }
    }

    func recover() throws {
        var failure: Error?
        for id in Array(entries.keys) {
            do { try restorePosition(id) } catch { failure = failure ?? error }
            do { try releaseOcclusion(id) } catch { failure = failure ?? error }
        }
        if let failure { throw failure }
    }
}

/// Private inherited pipes: no socket, persisted window data, or global daemon.
/// The child acknowledges ownership before its caller can move a window.
final class BackgroundWindowRecovery: @unchecked Sendable {
    static let shared = BackgroundWindowRecovery()
    static let command = "__computer-use-window-recovery"
    private let lock = NSLock()
    private var child: PrivateDesktopChild?
    private var failed = false

    private func start() throws -> SDKTransport {
        guard !failed else { throw SDKProtocolError("Window recovery process failed; close this task") }
        if let child, child.process.isRunning { return child.channel }
        guard child == nil else { failed = true; throw SDKProtocolError("Window recovery process exited") }
        let child = try PrivateDesktopChild(command: Self.command)
        self.child = child
        return child.channel
    }

    private func request(_ operation: String, windowID: CGWindowID? = nil, pid: pid_t? = nil) throws -> [String: Any] {
        try lock.withLock {
            let channel = try start()
            var message: [String: Any] = ["operation": operation]
            if let windowID { message["windowID"] = windowID }
            if let pid { message["pid"] = pid }
            do {
                try channel.write(SDKMessage(message))
                guard let reply = try channel.read(timeout: 5) else { throw SDKProtocolError("Window recovery disconnected") }
                if let error = reply.value["error"] as? String { throw ComputerUseError.stateUnavailable(error) }
                return reply.value
            } catch let error as ComputerUseError {
                // A failed restore leaves the child's record available for retry.
                throw error
            } catch {
                failed = true
                child?.disconnect()
                throw error
            }
        }
    }

    func rememberPosition(windowID: CGWindowID, pid: pid_t) throws -> CGPoint {
        let reply = try request("remember", windowID: windowID, pid: pid)
        guard let x = reply["x"] as? Double, let y = reply["y"] as? Double, x.isFinite, y.isFinite else {
            throw SDKProtocolError("Invalid recovery position")
        }
        return CGPoint(x: x, y: y)
    }

    func restorePosition(windowID: CGWindowID) throws { _ = try request("restore", windowID: windowID) }
    func pin(windowID: CGWindowID, pid: pid_t) throws -> Bool {
        let reply = try request("pin", windowID: windowID, pid: pid)
        guard let original = reply["original"] as? Bool else { throw SDKProtocolError("Invalid occlusion reply") }
        return original
    }
    func releaseOcclusion(windowID: CGWindowID) throws { _ = try request("release", windowID: windowID) }

    func createDisplay() throws -> CGDirectDisplayID {
        guard let id = try request("createDisplay")["displayID"] as? UInt32, id != 0 else {
            throw SDKProtocolError("Invalid virtual display identity")
        }
        return id
    }
    func destroyDisplay() throws { _ = try request("destroyDisplay") }

    func finish() throws {
        try lock.withLock {
            guard let child else { return }
            failed = true
            try child.finish()
        }
    }
}

public enum BackgroundWindowRecoveryRuntime {
    public static func finish() throws { try BackgroundWindowRecovery.shared.finish() }

    /// Returns before AppKit, proxying or any Locked Use acquisition is entered.
    public static func run(arguments: [String]) throws -> Bool {
        guard arguments == [BackgroundWindowRecovery.command] || arguments == [RecoveryVirtualDisplay.command] else { return false }
        signal(SIGPIPE, SIG_IGN)
        if arguments == [RecoveryVirtualDisplay.command] { try RecoveryVirtualDisplay.run(); return true }
        let ledger = WindowRecoveryLedger(backend: NativeWindowRecoveryBackend())
        let display = RecoveryVirtualDisplay()
        defer { try? display.destroy() }
        let channel = try SDKTransport(input: .standardInput, output: .standardOutput)
        var transportFailure: Error?
        do {
            while let request = try channel.read() {
                let reply: [String: Any]
                do {
                    guard let operation = request.value["operation"] as? String else { throw SDKProtocolError("Missing recovery operation") }
                    if operation == "createDisplay" {
                        guard ledger.entries.values.contains(where: { $0.position != nil }) else { throw SDKProtocolError("Register a window before creating its display") }
                        reply = ["displayID": try display.create()]
                    } else if operation == "destroyDisplay" {
                        guard !ledger.entries.values.contains(where: { $0.position != nil }) else { throw SDKProtocolError("Restore windows before removing their display") }
                        try display.destroy()
                        reply = ["ok": true]
                    } else {
                        guard let rawID = request.value["windowID"] as? NSNumber,
                              CFGetTypeID(rawID) != CFBooleanGetTypeID(), rawID.doubleValue > 0,
                              rawID.doubleValue <= Double(UInt32.max), rawID.doubleValue == Double(rawID.uint32Value) else {
                            throw SDKProtocolError("Invalid recovery request")
                        }
                        let id = rawID.uint32Value
                        switch operation {
                        case "remember", "pin":
                            guard let rawPID = request.value["pid"] as? NSNumber,
                                  CFGetTypeID(rawPID) != CFBooleanGetTypeID(), rawPID.doubleValue > 0,
                                  rawPID.doubleValue == Double(rawPID.int32Value) else { throw SDKProtocolError("Invalid recovery owner") }
                            if operation == "remember" {
                                let point = try ledger.rememberPosition(id, pid: rawPID.int32Value)
                                reply = ["x": point.x, "y": point.y]
                            } else { reply = ["original": try ledger.pin(id, pid: rawPID.int32Value)] }
                        case "restore": try ledger.restorePosition(id); reply = ["ok": true]
                        case "release": try ledger.releaseOcclusion(id); reply = ["ok": true]
                        default: throw SDKProtocolError("Unknown recovery operation")
                        }
                    }
                } catch { reply = ["error": String(describing: error)] }
                try channel.write(SDKMessage(reply))
            }
        } catch { transportFailure = error }
        // EOF also covers SIGKILL of the owner. Restore while its display is
        // still owned, then remove the display before acknowledging shutdown.
        let deadline = ProcessInfo.processInfo.systemUptime + 10
        repeat {
            do { try ledger.recover(); break }
            catch {
                if ProcessInfo.processInfo.systemUptime >= deadline { throw error }
                Thread.sleep(forTimeInterval: 0.1)
            }
        } while true
        try display.destroy()
        if let transportFailure { throw transportFailure }
        return true
    }
}

private final class NativeWindowRecoveryBackend: WindowRecoveryBackend {
    struct Target {
        let app: NSRunningApplication
        let launchedAt: Date?
        let element: AXUIElement
        let windowID: CGWindowID
    }
    private var targets: [CGWindowID: Target] = [:]

    func retain(windowID: CGWindowID, pid: pid_t) throws {
        if let target = targets[windowID] {
            guard target.app.processIdentifier == pid, try liveTarget(windowID) != nil else {
                throw SDKProtocolError("Recovery target no longer exists")
            }
            return
        }
        guard let app = NSRunningApplication(processIdentifier: pid), !app.isTerminated,
              !AppSafetyPolicy.isBlocked(bundleIdentifier: app.bundleIdentifier),
              let info = (CGWindowListCopyWindowInfo([.optionIncludingWindow], windowID) as? [[String: Any]])?.first,
              (info[kCGWindowOwnerPID as String] as? Int32) == pid else { throw SDKProtocolError("Recovery target no longer exists") }
        let root = AXUIElementCreateApplication(pid)
        AXUIElementSetMessagingTimeout(root, 0.2)
        var windows: CFTypeRef?
        guard AXUIElementCopyAttributeValue(root, kAXWindowsAttribute as CFString, &windows) == .success,
              let element = (windows as? [AXUIElement])?.first(where: { SkyLightSPI.shared.windowID(for: $0) == windowID })
                ?? SnapshotBuilder.windowElement(for: windowID, appElement: root) else { throw SDKProtocolError("Recovery window is not accessible") }
        AXUIElementSetMessagingTimeout(element, 0.2)
        targets[windowID] = Target(app: app, launchedAt: app.launchDate, element: element, windowID: windowID)
    }

    private func liveTarget(_ id: CGWindowID) throws -> Target? {
        guard let target = targets[id], !target.app.isTerminated else { return nil }
        guard let running = NSRunningApplication(processIdentifier: target.app.processIdentifier),
              running.launchDate == target.launchedAt else { return nil }
        let windows = CGWindowListCopyWindowInfo([.optionIncludingWindow], id) as? [[String: Any]]
        guard let windows else { throw SDKProtocolError("Recovery window inventory unavailable") }
        guard let info = windows.first, (info[kCGWindowOwnerPID as String] as? Int32) == target.app.processIdentifier else { return nil }
        guard let retainedID = SkyLightSPI.shared.windowID(for: target.element) else {
            throw SDKProtocolError("Recovery window identity is temporarily unavailable")
        }
        guard retainedID == id else { return nil }
        return target
    }

    func position(windowID: CGWindowID) throws -> CGPoint {
        guard let target = try liveTarget(windowID) else { throw SDKProtocolError("Recovery target closed") }
        var value: CFTypeRef?
        var point = CGPoint.zero
        guard AXUIElementCopyAttributeValue(target.element, kAXPositionAttribute as CFString, &value) == .success,
              let value, CFGetTypeID(value) == AXValueGetTypeID(),
              AXValueGetValue(value as! AXValue, .cgPoint, &point), point.x.isFinite, point.y.isFinite else {
            throw SDKProtocolError("Recovery position unavailable")
        }
        return point
    }

    func restore(windowID: CGWindowID, position: CGPoint) throws {
        guard let target = try liveTarget(windowID) else { return }
        var point = position
        guard let value = AXValueCreate(.cgPoint, &point),
              AXUIElementSetAttributeValue(target.element, kAXPositionAttribute as CFString, value) == .success else {
            if try liveTarget(windowID) == nil { return }
            throw SDKProtocolError("Recovery window could not be moved")
        }
        guard waitUntil(timeout: 2, {
            guard let actual = try? self.position(windowID: windowID) else { return false }
            return abs(actual.x - position.x) < 1 && abs(actual.y - position.y) < 1
        }) != nil else {
            if try liveTarget(windowID) == nil { return }
            throw SDKProtocolError("Recovery window position was not confirmed")
        }
    }

    func setOcclusion(windowID: CGWindowID, enabled: Bool) throws -> Bool {
        guard try liveTarget(windowID) != nil else { return enabled }
        return try SkyLightSPI.shared.setWindowOcclusionNotificationsEnabled(enabled, windowID: windowID)
    }
    func forget(windowID: CGWindowID) { targets.removeValue(forKey: windowID) }
}
