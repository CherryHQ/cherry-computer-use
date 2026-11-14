import AppKit
import ApplicationServices
import CoreGraphics
import Foundation
import OpenComputerUseVirtualDisplayShim

public enum WindowPlacement: String, CaseIterable, Sendable {
    case keep
    case agentDisplay = "agent_display"
    case restore
}

func parseWindowPlacement(_ rawValue: String?) throws -> WindowPlacement {
    let normalized = rawValue?
        .trimmingCharacters(in: .whitespacesAndNewlines)
        .lowercased() ?? WindowPlacement.keep.rawValue

    guard let placement = WindowPlacement(rawValue: normalized) else {
        let expected = WindowPlacement.allCases.map(\.rawValue).joined(separator: ", ")
        throw ComputerUseError.message(
            "Invalid window_placement '\(rawValue ?? "")'. Expected one of: \(expected)"
        )
    }

    return placement
}

/// Where a parked window goes on the agent display: inset from the top-left
/// corner, clamped so at least the top-left `minVisible` points stay inside the
/// display when the window is larger than it.
func agentDisplayPlacement(windowSize: CGSize, displayBounds: CGRect, inset: CGFloat = 40, minVisible: CGFloat = 200) -> CGPoint {
    let maxX = max(displayBounds.minX, displayBounds.maxX - max(windowSize.width, minVisible))
    let maxY = max(displayBounds.minY, displayBounds.maxY - max(windowSize.height, minVisible))
    return CGPoint(
        x: min(displayBounds.minX + inset, maxX),
        y: min(displayBounds.minY + inset, maxY)
    )
}

/// Quartz disallows gaps between displays. Touch the outer bottom-right corner
/// of the physical layout to minimize pointer crossings, without
/// placing the invisible screen between the user's displays.
func agentDisplayOrigin(displaysBefore: [CGRect], displaysNow: [CGRect], agentBounds: CGRect) -> CGPoint? {
    guard let rightmost = displaysBefore.max(by: { ($0.maxX, $0.maxY) < ($1.maxX, $1.maxY) }) else {
        return nil
    }
    let origin = CGPoint(x: rightmost.maxX, y: rightmost.maxY)
    return displaysBefore == displaysNow && agentBounds.origin == origin ? nil : origin
}

/// Windows macOS moved onto a new agent display: on one of the user's displays
/// before the agent display was created, and moved onto the agent display since.
/// Each maps to the origin it had before. A window that reached into the agent
/// display's area is pulled onto its own display instead (macOS leaves a window
/// it took off the last agent display hanging past the main display's edge, so
/// back at its origin most of it would still be on the agent display). A frame
/// on none of the user's displays is not a place to return to (frames read while
/// Spaces switch are shifted off them).
func windowsMovedOntoAgentDisplay(_ displayBounds: CGRect, userDisplays: [CGRect], before: [CGWindowID: CGRect], now: [CGWindowID: CGRect]) -> [CGWindowID: CGPoint] {
    before.reduce(into: [:]) { moved, window in
        let old = window.value
        let area = { (display: CGRect) in display.intersection(old).width * display.intersection(old).height }
        guard let home = userDisplays.filter({ $0.intersects(old) }).max(by: { area($0) < area($1) }),
              let frame = now[window.key], frame.origin != old.origin, displayBounds.intersects(frame)
        else { return }
        moved[window.key] = !displayBounds.intersects(old) ? old.origin : CGPoint(
            x: max(home.minX, min(old.minX, home.maxX - old.width)),
            y: max(home.minY, min(old.minY, home.maxY - old.height))
        )
    }
}

/// The agent's own display: a virtual display (the mechanism behind Screen
/// Sharing's headless sessions) that the user never sees. A window parked on it
/// is genuinely on screen for WindowServer, so its app renders it, exposes its
/// accessibility tree and accepts input. Parking uses AX position without
/// activation; a separate recovery child retains the original position across
/// owner exit. Display topology still changes temporarily during this operation.
final class AgentDisplay: @unchecked Sendable {
    static let shared = AgentDisplay()

    static let displaySize = CGSize(width: 1920, height: 1080)
    // Upper bounds for the polls; the observed values are far lower and are
    // logged with OPEN_COMPUTER_USE_DEBUG_TIMING=1.
    static let displaySettle: TimeInterval = 3.0
    static let moveSettle: TimeInterval = 2.0
    // Fallback sleeps when the observation SPIs are unavailable.
    static let displayFallbackSettle: TimeInterval = 0.5
    static let moveFallbackSettle: TimeInterval = 1.0

    struct ParkedWindow {
        let pid: pid_t
    }

    private let lock = NSLock()
    private(set) var displayID: CGDirectDisplayID = 0
    private(set) var displaySpaceIDs: Set<UInt64> = []
    private var parked: [CGWindowID: ParkedWindow] = [:]
    private var exitHookInstalled = false

    var isSupported: Bool {
        ocu_virtual_display_is_supported() != 0
    }

    var displayBounds: CGRect? {
        displayID == 0 ? nil : CGDisplayBounds(displayID)
    }

    func isParked(windowID: CGWindowID) -> Bool {
        lock.lock()
        defer { lock.unlock() }
        return parked[windowID] != nil
    }

    func parkedWindowIDs(pid: pid_t) -> [CGWindowID] {
        lock.lock()
        defer { lock.unlock() }
        return parked.filter { $0.value.pid == pid }.map(\.key)
    }

    /// Move `window` onto the agent display. Returns the display bounds.
    @discardableResult
    func park(windowID: CGWindowID, pid: pid_t, window: AXUIElement, enabled: Bool? = nil, cancellation: SDKCancellation? = nil) throws -> CGRect {
        lock.lock()
        defer { lock.unlock() }
        try (enabled.map { VirtualDisplayConfig(enabled: $0) } ?? .current).requireEnabled()
        if cancellation?.isCancelled == true { throw SDKDomainError("CANCELLED", "Window placement cancelled") }
        if parked[windowID] != nil {
            _ = try BackgroundWindowRecovery.shared.rememberPosition(windowID: windowID, pid: pid)
            return try ensureDisplay(enabled: enabled)
        }
        // Register before display creation: WindowServer can move remembered
        // windows as soon as the display appears, before our explicit AX move.
        _ = try BackgroundWindowRecovery.shared.rememberPosition(windowID: windowID, pid: pid)
        let size = axSize(window) ?? CGSize(width: 800, height: 600)
        let start = TimingLog.now()
        parked[windowID] = ParkedWindow(pid: pid)
        installExitHookIfNeeded()
        do {
            let bounds = try ensureDisplay(enabled: enabled)
            if cancellation?.isCancelled == true { throw SDKDomainError("CANCELLED", "Window placement cancelled") }
            try setAXPosition(window, agentDisplayPlacement(windowSize: size, displayBounds: bounds))
            try waitForWindow(windowID, toLandOn: displaySpaceIDs, or: bounds, cancellation: cancellation)
            TimingLog.log("agent_display.park", since: start)
            return bounds
        } catch {
            do {
                try BackgroundWindowRecovery.shared.restorePosition(windowID: windowID)
                if parked.count == 1 { try destroyDisplay() }
                parked.removeValue(forKey: windowID)
            } catch {
                throw SDKDomainError("CLEANUP_FAILED", "Window placement failed and its original position could not be restored", effect: "possible")
            }
            throw error
        }
    }

    /// Wait until the window's frame sits inside the display and, when the
    /// Space SPI is available, WindowServer also reports it on one of the
    /// display's Spaces. Space membership alone precedes the frame update.
    private func waitForWindow(_ windowID: CGWindowID, toLandOn spaceIDs: Set<UInt64>, or bounds: CGRect, cancellation: SDKCancellation?) throws {
        let spi = SkyLightSPI.shared
        let isOnDisplay = {
            guard let frame = Self.windowFrame(windowID),
                  bounds.contains(CGPoint(x: frame.minX + 1, y: frame.minY + 1))
            else { return false }
            if !spaceIDs.isEmpty, let spaces = spi.spaces(forWindow: windowID) {
                return spaces.contains(where: spaceIDs.contains)
            }
            return true
        }
        let deadline = ProcessInfo.processInfo.systemUptime + Self.moveSettle + Self.moveFallbackSettle
        repeat {
            if cancellation?.isCancelled == true { throw SDKDomainError("CANCELLED", "Window placement cancelled", effect: "possible") }
            if isOnDisplay() { return }
            Thread.sleep(forTimeInterval: 0.01)
        } while ProcessInfo.processInfo.systemUptime < deadline
        throw ComputerUseError.stateUnavailable("The window did not confirm placement on the agent display")
    }

    private static func windowFrame(_ windowID: CGWindowID) -> CGRect? {
        let infoList = CGWindowListCopyWindowInfo([.optionAll], kCGNullWindowID) as? [[String: Any]] ?? []
        for info in infoList {
            guard let number = info[kCGWindowNumber as String] as? NSNumber, number.uint32Value == windowID,
                  let dictionary = info[kCGWindowBounds as String] as? NSDictionary,
                  let bounds = CGRect(dictionaryRepresentation: dictionary) else { continue }
            return bounds
        }
        return nil
    }

    static func isRestored(frame: CGRect, to originalPosition: CGPoint, tolerance: CGFloat = 1) -> Bool {
        abs(frame.minX - originalPosition.x) <= tolerance
            && abs(frame.minY - originalPosition.y) <= tolerance
    }

    /// Put a parked window back where it was.
    func restore(windowID: CGWindowID) throws {
        lock.lock()
        defer { lock.unlock() }
        guard parked[windowID] != nil else {
            return
        }
        let start = TimingLog.now()
        try BackgroundWindowRecovery.shared.restorePosition(windowID: windowID)
        if parked.count == 1 { try destroyDisplay() }
        parked.removeValue(forKey: windowID)
        TimingLog.log("agent_display.restore", since: start)
    }

    func restoreAll() {
        lock.lock()
        let windowIDs = Array(parked.keys)
        lock.unlock()
        for windowID in windowIDs {
            try? restore(windowID: windowID)
        }
    }

    /// Restore every window parked for one application. Unlike process-exit
    /// cleanup, an explicit restore reports a live failure to the caller after
    /// attempting the remaining windows.
    func restoreAll(pid: pid_t) throws {
        let windowIDs = parkedWindowIDs(pid: pid)
        var firstError: Error?
        for windowID in windowIDs {
            do {
                try restore(windowID: windowID)
            } catch {
                firstError = firstError ?? error
            }
        }
        if let firstError {
            throw firstError
        }
    }

    // MARK: - Display lifecycle (call with lock held)

    private func ensureDisplay(enabled: Bool?) throws -> CGRect {
        try (enabled.map { VirtualDisplayConfig(enabled: $0) } ?? .current).requireEnabled()
        if displayID != 0, let bounds = displayBounds, !bounds.isEmpty {
            return bounds
        }
        guard isSupported else {
            throw ComputerUseError.message("window_placement 'agent_display' is unavailable: CGVirtualDisplay is not present on this macOS")
        }
        let spi = SkyLightSPI.shared
        let spacesBefore = Set((spi.managedDisplaySpaces() ?? [:]).values.flatMap { $0 })
        let displaysBefore = Self.activeDisplays().map { (id: $0, bounds: CGDisplayBounds($0)) }
        let windowsBefore = Self.appWindows()
        let start = TimingLog.now()
        let id = try BackgroundWindowRecovery.shared.createDisplay()
        displayID = id
        TimingLog.log("agent_display.create", since: start)

        // Ready means WindowServer reports the display's bounds and has given
        // it a Space; without the Space SPI, fall back to a fixed settle.
        var newSpaces: Set<UInt64> = []
        let ready = waitUntil(timeout: Self.displaySettle) {
            guard let bounds = self.displayBounds, !bounds.isEmpty else { return false }
            guard let displays = spi.managedDisplaySpaces() else { return true }
            newSpaces = Set(displays.values.flatMap { $0 }).subtracting(spacesBefore)
            return !newSpaces.isEmpty
        }
        if ready == nil, displayBounds?.isEmpty != false {
            throw ComputerUseError.message("window_placement 'agent_display' timed out waiting for the agent display")
        }
        if newSpaces.isEmpty {
            Thread.sleep(forTimeInterval: Self.displayFallbackSettle)
        }
        displaySpaceIDs = newSpaces
        guard displaysBefore.allSatisfy({ CGDisplayBounds($0.id) == $0.bounds }) else {
            throw ComputerUseError.stateUnavailable("Existing display arrangement changed while creating the agent display")
        }
        returnRememberedWindows(windowsBefore, userDisplays: displaysBefore.map(\.bounds))
        TimingLog.log("agent_display.ready", since: start)
        return displayBounds ?? .zero
    }

    /// Put the user's displays back where they were when macOS pushed them
    /// aside for the agent display, and the agent display past the rightmost
    /// (see `agentDisplayOrigin`). A display mirroring another is left alone:
    /// setting its origin would take it out of the mirror set.
    static func placeBesideUserDisplays(_ id: CGDirectDisplayID, displaysBefore: [(id: CGDirectDisplayID, bounds: CGRect)]) throws {
        let theirs = displaysBefore.filter { CGDisplayMirrorsDisplay($0.id) == kCGNullDirectDisplay }
        guard !theirs.isEmpty else { throw ComputerUseError.stateUnavailable("No physical display is available for window restoration") }
        guard let origin = agentDisplayOrigin(displaysBefore: theirs.map(\.bounds), displaysNow: theirs.map { CGDisplayBounds($0.id) }, agentBounds: CGDisplayBounds(id)) else {
            return
        }
        var config: CGDisplayConfigRef?
        guard CGBeginDisplayConfiguration(&config) == .success, let config else {
            throw ComputerUseError.stateUnavailable("Could not preserve the physical display arrangement")
        }
        for display in theirs {
            guard CGConfigureDisplayOrigin(config, display.id, Int32(display.bounds.minX), Int32(display.bounds.minY)) == .success else {
                CGCancelDisplayConfiguration(config)
                throw ComputerUseError.stateUnavailable("The physical display arrangement changed during placement")
            }
        }
        guard CGConfigureDisplayOrigin(config, id, Int32(origin.x), Int32(origin.y)) == .success else {
            CGCancelDisplayConfiguration(config)
            throw ComputerUseError.stateUnavailable("Could not isolate the virtual display from physical displays")
        }
        guard CGCompleteDisplayConfiguration(config, .forSession) == .success,
              waitUntil(timeout: Self.displaySettle, {
                  CGDisplayBounds(id).origin == origin && theirs.allSatisfy { CGDisplayBounds($0.id) == $0.bounds }
              }) != nil else {
            let actual = CGDisplayBounds(id).origin
            let preserved = theirs.allSatisfy { CGDisplayBounds($0.id) == $0.bounds }
            throw ComputerUseError.stateUnavailable("Display arrangement was not confirmed (requested \(origin), actual \(actual), existing displays preserved: \(preserved)); virtual display removed")
        }
    }

    /// Defend against WindowServer moving unrelated windows during display
    /// creation, even though each agent display now has a fresh identity.
    private func returnRememberedWindows(_ before: [CGWindowID: (pid: pid_t, frame: CGRect)], userDisplays: [CGRect]) {
        guard let bounds = displayBounds else { return }
        let moved = windowsMovedOntoAgentDisplay(
            bounds,
            userDisplays: userDisplays,
            before: before.filter { parked[$0.key] == nil }.mapValues(\.frame),
            now: Self.appWindows().mapValues(\.frame)
        )
        for (windowID, origin) in moved {
            var windows: CFTypeRef?
            guard let pid = before[windowID]?.pid,
                  AXUIElementCopyAttributeValue(AXUIElementCreateApplication(pid), kAXWindowsAttribute as CFString, &windows) == .success,
                  let element = (windows as? [AXUIElement])?.first(where: { SkyLightSPI.shared.windowID(for: $0) == windowID })
            else { continue }
            try? setAXPosition(element, origin)
        }
    }

    static func activeDisplays() -> [CGDirectDisplayID] {
        var count: UInt32 = 0
        guard CGGetActiveDisplayList(0, nil, &count) == .success, count > 0 else { return [] }
        var ids = [CGDirectDisplayID](repeating: 0, count: Int(count))
        guard CGGetActiveDisplayList(count, &ids, &count) == .success else { return [] }
        return Array(ids.prefix(Int(count)))
    }

    /// App windows and their frames, keyed by window ID, on every Space: macOS
    /// also moves a window on a Space the user is not looking at (behind a
    /// full-screen app) onto the agent display.
    private static func appWindows() -> [CGWindowID: (pid: pid_t, frame: CGRect)] {
        let infoList = CGWindowListCopyWindowInfo([.optionAll, .excludeDesktopElements], kCGNullWindowID) as? [[String: Any]] ?? []
        var windows: [CGWindowID: (pid: pid_t, frame: CGRect)] = [:]
        for info in infoList where (info[kCGWindowLayer as String] as? Int) == 0 {
            guard let number = info[kCGWindowNumber as String] as? NSNumber,
                  let pid = info[kCGWindowOwnerPID as String] as? pid_t,
                  let dictionary = info[kCGWindowBounds as String] as? NSDictionary,
                  let frame = CGRect(dictionaryRepresentation: dictionary) else { continue }
            windows[number.uint32Value] = (pid, frame)
        }
        return windows
    }

    private func destroyDisplay() throws {
        // The child owns the private object. Process exit also clears display
        // state that WindowServer can retain after a plain object release.
        try BackgroundWindowRecovery.shared.destroyDisplay()
        displayID = 0
        displaySpaceIDs = []
    }

    private func installExitHookIfNeeded() {
        guard !exitHookInstalled else { return }
        exitHookInstalled = true
        atexit {
            AgentDisplay.shared.restoreAll()
        }
    }

    // MARK: - AX helpers

    private func axSize(_ element: AXUIElement) -> CGSize? {
        var value: CFTypeRef?
        guard AXUIElementCopyAttributeValue(element, kAXSizeAttribute as CFString, &value) == .success, let value else { return nil }
        var size = CGSize.zero
        guard AXValueGetValue(value as! AXValue, .cgSize, &size) else { return nil }
        return size
    }

    private func setAXPosition(_ element: AXUIElement, _ point: CGPoint) throws {
        var mutable = point
        guard let value = AXValueCreate(.cgPoint, &mutable) else {
            throw ComputerUseError.message("could not encode the window position")
        }
        let result = AXUIElementSetAttributeValue(element, kAXPositionAttribute as CFString, value)
        guard result == .success else {
            throw ComputerUseError.stateUnavailable("window_placement could not move the window (AXError \(result.rawValue))")
        }
    }
}
