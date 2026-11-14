import Foundation
import IOKit.hid

struct MonitoredInputKinds: OptionSet, Sendable {
    let rawValue: Int
    static let keyboard = Self(rawValue: 1)
    static let pointer = Self(rawValue: 2)
    static let consumer = Self(rawValue: 4)
}

struct MonitoredInputDevice {
    let id: UInt64
    let kinds: MonitoredInputKinds
}

/// Backend callbacks run on the main run loop. No key values or device identity
/// are exposed to the monitor; IDs are only process-local resource handles.
@MainActor
protocol PhysicalInputBackend: AnyObject {
    var inputMonitoringAllowed: Bool { get }
    func start(input: @escaping @MainActor (IOReturn) -> Void,
               topologyChanged: @escaping @MainActor () -> Void) throws -> [MonitoredInputDevice]
    func open(_ id: UInt64) -> IOReturn
    func observe(_ id: UInt64)
    func stop()
}

/// Supplement the filtering CG tap with HID activity detection. A forwarding
/// virtual device can supply an input category, but opening it alone does not
/// prove that the seizing tool forwards every physical device / Secure Input.
@MainActor
final class PhysicalInputMonitor {
    private enum State { case stopped, starting, healthy, failed }
    private var state = State.stopped
    private var generation: UUID?
    private let backend: any PhysicalInputBackend
    private let activity: @MainActor () -> Void
    private let failure: @MainActor () -> Void
    private(set) var observedDeviceCount = 0
    private(set) var seizedDeviceCount = 0
    var healthy: Bool {
        if state == .healthy && !backend.inputMonitoringAllowed { invalidate() }
        return state == .healthy
    }

    init(backend: any PhysicalInputBackend = NativePhysicalInputBackend(),
         activity: @escaping @MainActor () -> Void, failure: @escaping @MainActor () -> Void) {
        self.backend = backend; self.activity = activity; self.failure = failure
    }

    func start() throws {
        guard state == .stopped else { throw GuardianError.message("Hardware input monitor already started") }
        guard backend.inputMonitoringAllowed else { throw GuardianError.message("Input Monitoring missing") }
        state = .starting
        observedDeviceCount = 0
        seizedDeviceCount = 0
        let generation = UUID()
        self.generation = generation
        do {
            let devices = try backend.start(input: { [weak self] result in
                guard let self, self.generation == generation else { return }
                if result != kIOReturnSuccess { self.invalidate() }
                else if self.state == .starting {
                    // Input during arming is a takeover too. Do not acknowledge
                    // readiness and then unlock after dropping that event.
                    self.invalidate()
                } else if self.state == .healthy { self.activity() }
            }, topologyChanged: { [weak self] in
                guard let self, self.generation == generation else { return }
                self.invalidate()
            })
            var required: MonitoredInputKinds = []
            var observed: MonitoredInputKinds = []
            var opened: [UInt64] = []
            for device in devices {
                guard !device.kinds.isEmpty else { throw GuardianError.message("Unknown hardware input category") }
                required.formUnion(device.kinds)
                let result = backend.open(device.id)
                if result == kIOReturnSuccess {
                    observed.formUnion(device.kinds)
                    opened.append(device.id)
                } else if result == kIOReturnExclusiveAccess {
                    seizedDeviceCount += 1
                } else {
                    throw GuardianError.message("Hardware input device could not be opened")
                }
            }
            // A consumer-control interface alone cannot stand in for a keyboard
            // or pointer. Never accept partial category coverage after a seize.
            guard !observed.intersection([.keyboard, .pointer]).isEmpty,
                  observed.isSuperset(of: required) else {
                throw GuardianError.message("Hardware input categories are not fully observable")
            }
            for id in opened { backend.observe(id) }
            guard state == .starting else { throw GuardianError.message("Hardware input changed during startup") }
            observedDeviceCount = opened.count
            state = .healthy
        } catch {
            stop()
            throw error
        }
    }

    private func invalidate() {
        guard state == .starting || state == .healthy else { return }
        let notify = state == .healthy
        state = .failed
        // Latch failure before notifying the owner, which may stop the monitor
        // reentrantly. No later activity callback can restore health.
        if notify { failure() }
    }

    func stop() {
        guard state != .stopped else { return }
        state = .stopped
        generation = nil
        backend.stop()
    }
}
