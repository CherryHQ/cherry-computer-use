import AppKit
import ApplicationServices
@testable import OpenComputerUseKit

/// Real Desktop setup for opt-in tests. Dock performs the changes through AX;
/// SkyLight is used only to read the resulting display/Space identities.
@MainActor
final class MissionControlTestSupport {
    private typealias Notify = @convention(c) (CFString, Int32) -> Int32
    private let dock: AXUIElement
    private let displayID: CGDirectDisplayID
    private let readDisplay: () throws -> [String: Any]
    private let notify: Notify
    private(set) var createdSpace: UInt64?

    init(displayIdentifier: String, readDisplay: @escaping () throws -> [String: Any]) throws {
        guard let application = NSWorkspace.shared.runningApplications.first(where: { $0.bundleIdentifier == "com.apple.dock" }),
              let handle = dlopen(nil, RTLD_LAZY), let symbol = dlsym(handle, "CoreDockSendNotification") else {
            throw ComputerUseError.message("Mission Control test setup is unavailable")
        }
        dock = AXUIElementCreateApplication(application.processIdentifier)
        AXUIElementSetMessagingTimeout(dock, 0.5)
        notify = unsafeBitCast(symbol, to: Notify.self)
        self.readDisplay = readDisplay
        var displays = [CGDirectDisplayID](repeating: 0, count: 32), count: UInt32 = 0
        guard CGGetOnlineDisplayList(32, &displays, &count) == .success,
              let id = displays.prefix(Int(count)).first(where: { id in
                  if displayIdentifier == "Main" { return id == CGMainDisplayID() }
                  guard let uuid = CGDisplayCreateUUIDFromDisplayID(id)?.takeRetainedValue() else { return false }
                  return (CFUUIDCreateString(nil, uuid) as String).caseInsensitiveCompare(displayIdentifier) == .orderedSame
              }) else { throw ComputerUseError.message("Cannot bind Mission Control to the test display") }
        displayID = id
    }

    func alternateDesktop(from home: UInt64) throws -> UInt64 {
        if let existing = try spaces().first(where: { $0.type == 0 && $0.id != home }) { return existing.id }
        let before = Set(try spaces().map(\.id))
        try withMissionControl {
            try press(subgroup("mc.spaces.add"), action: kAXPressAction)
            try wait("new ordinary Desktop") {
                let added = try self.spaces().filter { !before.contains($0.id) }
                guard added.count == 1, added[0].type == 0 else { return false }
                self.createdSpace = added[0].id
                return true
            }
        }
        guard let createdSpace else { throw ComputerUseError.message("Desktop creation was not confirmed") }
        print("cross-space setup: created temporary Desktop \(createdSpace) on display \(displayID)")
        return createdSpace
    }

    func activate(_ space: UInt64) throws {
        if try currentSpace() == space { return }
        try withMissionControl {
            try press(button(for: space), action: kAXPressAction)
            try wait("Desktop activation") { try self.currentSpace() == space && self.missionControl() == nil }
        }
        RunLoop.current.run(until: Date().addingTimeInterval(0.5))
    }

    /// The caller must first stop its fixtures and verify that this Desktop is empty.
    func removeCreatedDesktop() throws {
        guard let createdSpace else { return }
        guard try currentSpace() != createdSpace, try spaces().filter({ $0.type == 0 }).count > 1 else {
            throw ComputerUseError.message("Refusing to remove the active or only Desktop")
        }
        try withMissionControl { try press(button(for: createdSpace), action: "AXRemoveDesktop") }
        try wait("temporary Desktop removal") { try !self.spaces().contains { $0.id == createdSpace } }
        self.createdSpace = nil
        print("cross-space cleanup: removed temporary Desktop \(createdSpace)")
    }

    private func spaces() throws -> [(id: UInt64, type: Int)] {
        guard let values = try readDisplay()["Spaces"] as? [[String: Any]] else {
            throw ComputerUseError.message("Managed display no longer exposes its Spaces")
        }
        return try values.map {
            guard let id = $0["id64"] as? NSNumber, let type = $0["type"] as? NSNumber else {
                throw ComputerUseError.message("Unrecognized managed Space identity")
            }
            return (id.uint64Value, type.intValue)
        }
    }

    private func currentSpace() throws -> UInt64? {
        (try readDisplay()["Current Space"] as? [String: Any])?["id64"] as? UInt64
    }

    private func value(_ element: AXUIElement, _ name: String) -> CFTypeRef? {
        var result: CFTypeRef?
        return AXUIElementCopyAttributeValue(element, name as CFString, &result) == .success ? result : nil
    }

    private func children(_ element: AXUIElement) -> [AXUIElement] { value(element, kAXChildrenAttribute) as? [AXUIElement] ?? [] }
    private func identifier(_ element: AXUIElement) -> String? { value(element, "AXIdentifier") as? String }
    private func missionControl() -> AXUIElement? { children(dock).first { identifier($0) == "mc" } }

    private func subgroup(_ name: String) throws -> AXUIElement {
        guard let mc = missionControl(),
              let display = children(mc).first(where: { identifier($0) == "mc.display" && (value($0, "AXDisplayID") as? NSNumber)?.uint32Value == displayID }),
              let spaces = children(display).first(where: { identifier($0) == "mc.spaces" }),
              let target = children(spaces).first(where: { identifier($0) == name }) else {
            throw ComputerUseError.message("Mission Control does not expose \(name) for display \(displayID)")
        }
        return target
    }

    private func button(for space: UInt64) throws -> AXUIElement {
        let buttons = try children(subgroup("mc.spaces.list"))
        let ids = try spaces().map(\.id)
        guard buttons.count == ids.count, let index = ids.firstIndex(of: space) else {
            throw ComputerUseError.message("Mission Control buttons do not match the current managed Spaces")
        }
        return buttons[index]
    }

    private func press(_ element: AXUIElement, action: String) throws {
        let result = AXUIElementPerformAction(element, action as CFString)
        guard result == .success else { throw ComputerUseError.message("Mission Control \(action) failed: \(result.rawValue)") }
    }

    private func withMissionControl(_ body: () throws -> Void) throws {
        if missionControl() == nil { _ = notify("com.apple.expose.awake" as CFString, 0) }
        defer {
            if missionControl() != nil {
                _ = notify("com.apple.expose.awake" as CFString, 0)
                RunLoop.current.run(until: Date().addingTimeInterval(0.5))
            }
        }
        try wait("Mission Control accessibility group") { self.missionControl() != nil }
        try wait("Mission Control display controls") {
            guard let list = try? self.subgroup("mc.spaces.list"),
                  (try? self.subgroup("mc.spaces.add")) != nil else { return false }
            return try self.children(list).count == self.spaces().count
        }
        RunLoop.current.run(until: Date().addingTimeInterval(0.3))
        try body()
    }

    private func wait(_ description: String, until predicate: () throws -> Bool) throws {
        let deadline = Date().addingTimeInterval(4)
        while Date() < deadline {
            if try predicate() { return }
            RunLoop.current.run(until: Date().addingTimeInterval(0.05))
        }
        throw ComputerUseError.message("Timed out waiting for \(description)")
    }
}
