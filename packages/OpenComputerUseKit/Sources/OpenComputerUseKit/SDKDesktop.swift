import AppKit
import ApplicationServices
import Foundation
import ScreenCaptureKit

public struct SDKDomainError: Error, Sendable {
    public let code: String
    public let message: String
    public let effect: String
    public init(_ code: String, _ message: String, effect: String = "none") {
        self.code = code; self.message = message; self.effect = effect
    }
}

public protocol SDKDesktopBackend: Sendable {
    func capabilities() -> [String: [String: Any]]
    func call(_ method: String, params: [String: Any], cancellation: SDKCancellation) throws -> Any
    func close() throws
    func listAppSessions() -> [[String: Any]]
    func prepareAppStop(_ params: [String: Any]) throws -> @Sendable () throws -> SDKMessage
}

public extension SDKDesktopBackend {
    func listAppSessions() -> [[String: Any]] { [] }
    func prepareAppStop(_ params: [String: Any]) throws -> @Sendable () throws -> SDKMessage {
        throw SDKDomainError("UNSUPPORTED_CAPABILITY", "Application sessions are not connected")
    }
}

public final class MacOSSDKDesktop: SDKDesktopBackend, @unchecked Sendable {
    fileprivate struct Record {
        let element: AXUIElement
        let name: String
        let role: String
        let action: String?
    }
    fileprivate struct Snapshot {
        let app: RunningAppDescriptor
        let window: AXUIElement
        let title: String
        let elements: [String: Record]
        let options: [String: Any]
    }
    private var apps: [String: RunningAppDescriptor] = [:]
    final class AppControl: @unchecked Sendable {
        let id = UUID().uuidString
        let appID: String
        let app: RunningAppDescriptor
        var status = "active"
        fileprivate var snapshots: [String: Snapshot] = [:]
        var cancellation: SDKCancellation?
        let running = DispatchGroup()
        init(appID: String, app: RunningAppDescriptor) { self.appID = appID; self.app = app }
        var info: [String: Any] { ["id": id, "app": ["id": appID, "name": app.name], "status": status] }
    }
    private let controlLock = NSLock()
    private var controls: [String: AppControl] = [:]
    private var uncertain = false

    public init() {}

    public func capabilities() -> [String: [String: Any]] {
        func availability(_ granted: Bool) -> [String: Any] {
            granted ? ["status": "available"] : ["status": "unavailable", "reason": ["code": "PERMISSION_REQUIRED", "message": "Permission has not been granted to this app agent"]]
        }
        return ["accessibility": availability(AXIsProcessTrusted()), "click": availability(AXIsProcessTrusted()), "screenshot": availability(CGPreflightScreenCaptureAccess())]
    }

    public func close() throws {
        controlLock.withLock { controls.removeAll() }
        apps.removeAll()
    }

    public func listAppSessions() -> [[String: Any]] {
        controlLock.withLock { controls.values.sorted { $0.id < $1.id }.map(\.info) }
    }

    func openAppSession(appID: String, app: RunningAppDescriptor) throws -> [String: Any] {
        try controlLock.withLock {
            if let existing = controls.values.first(where: { $0.appID == appID && $0.status != "stopped" }) {
                guard existing.status == "active" else { throw SDKDomainError("APP_SESSION_STOPPED", "Application control is stopping") }
                return existing.info
            }
            let control = AppControl(appID: appID, app: app)
            controls[control.id] = control
            return control.info
        }
    }

    func beginAppRequest(_ id: String, cancellation: SDKCancellation) throws -> AppControl {
        try controlLock.withLock {
            guard let control = controls[id] else { throw SDKDomainError("APP_SESSION_NOT_FOUND", "Application session belongs to another runtime or does not exist") }
            guard control.status == "active" else { throw SDKDomainError("APP_SESSION_STOPPED", "Application control has stopped") }
            control.cancellation = cancellation
            control.running.enter()
            return control
        }
    }

    func endAppRequest(_ control: AppControl) {
        controlLock.withLock {
            control.cancellation = nil
            control.running.leave()
        }
    }

    public func prepareAppStop(_ params: [String: Any]) throws -> @Sendable () throws -> SDKMessage {
        guard Set(params.keys) == ["appSessionId"], let id = params["appSessionId"] as? String, !id.isEmpty else {
            throw SDKDomainError("INVALID_ARGUMENT", "Expected an application session ID")
        }
        let control = try controlLock.withLock {
            guard let control = controls[id] else { throw SDKDomainError("APP_SESSION_NOT_FOUND", "Application session belongs to another runtime or does not exist") }
            if control.status == "active" { control.status = "stopping" }
            control.cancellation?.cancel()
            return control
        }
        return {
            guard control.running.wait(timeout: .now() + 1) == .success else {
                self.controlLock.withLock { self.uncertain = true }
                throw SDKDomainError("CLEANUP_FAILED", "Application request did not settle; cleanup is unconfirmed", effect: "possible")
            }
            return self.controlLock.withLock {
                control.snapshots.removeAll()
                control.status = "stopped"
                return SDKMessage(["appSessionId": id, "status": "stopped", "cleanup": "complete"])
            }
        }
    }

    public func call(_ method: String, params: [String: Any], cancellation: SDKCancellation) throws -> Any {
        guard !controlLock.withLock({ uncertain }) else { throw SDKDomainError("CLOSED", "An earlier action has an uncertain outcome; start a new session") }
        try check(cancellation)
        switch method {
        case "listApps":
            guard params.isEmpty else { throw SDKDomainError("INVALID_ARGUMENT", "Expected empty parameters") }
            apps = apps.filter { !$0.value.runningApplication.isTerminated }
            controlLock.withLock {
                for control in controls.values where apps[control.appID] == nil {
                    control.snapshots.removeAll()
                    control.status = "stopped"
                }
            }
            return AppDiscovery.runningApps().filter {
                $0.runningApplication.activationPolicy == .regular && !AppSafetyPolicy.isBlocked(bundleIdentifier: $0.bundleIdentifier)
            }.map { app -> [String: Any] in
                let id = apps.first(where: { $0.value.runningApplication == app.runningApplication })?.key ?? UUID().uuidString
                apps[id] = app
                return ["id": id, "name": app.name]
            }
        case "openAppSession":
            guard Set(params.keys) == ["appId"], let appID = params["appId"] as? String, !appID.isEmpty else {
                throw SDKDomainError("INVALID_ARGUMENT", "Expected an application ID")
            }
            guard let app = apps[appID], !app.runningApplication.isTerminated else { throw SDKDomainError("TARGET_UNAVAILABLE", "Application ID is not in this runtime") }
            return try openAppSession(appID: appID, app: app)
        case "getAppState":
            try validateObservation(params)
            let control = try beginAppRequest(params["appSessionId"] as! String, cancellation: cancellation)
            defer { endAppRequest(control) }
            return try observe(params, control: control, cancellation: cancellation)
        case "act":
            guard params["type"] as? String == "click" else { throw SDKDomainError("UNSUPPORTED_CAPABILITY", "Only element click is connected") }
            guard Set(params.keys).isSubset(of: ["type", "appSessionId", "snapshotId", "elementId", "button", "count", "allowGlobalInput", "x", "y"]),
                  let snapshotID = params["snapshotId"] as? String, !snapshotID.isEmpty,
                  let appSessionID = params["appSessionId"] as? String, !appSessionID.isEmpty else {
                throw SDKDomainError("INVALID_ARGUMENT", "Invalid click parameters")
            }
            if let allow = params["allowGlobalInput"], !(allow is NSNumber && CFGetTypeID(allow as CFTypeRef) == CFBooleanGetTypeID()) {
                throw SDKDomainError("INVALID_ARGUMENT", "allowGlobalInput must be boolean")
            }
            let count = try integer(params["count"], fallback: 1)
            guard count <= 3, ["left", "right", "middle"].contains(params["button"] as? String ?? "left") else { throw SDKDomainError("INVALID_ARGUMENT", "Invalid click count or button") }
            guard let elementID = params["elementId"] as? String, !elementID.isEmpty, params["x"] == nil, params["y"] == nil,
                  count == 1, params["button"] == nil || params["button"] as? String == "left" else {
                throw SDKDomainError("UNSUPPORTED_CAPABILITY", "This backend supports one semantic left click on an element")
            }
            let control = try beginAppRequest(appSessionID, cancellation: cancellation)
            defer { endAppRequest(control) }
            guard let snapshot = control.snapshots[snapshotID], let record = snapshot.elements[elementID] else {
                throw SDKDomainError("STALE_SNAPSHOT", "Snapshot or element is expired or belongs to another session")
            }
            guard let action = record.action else { throw SDKDomainError("UNSUPPORTED_CAPABILITY", "Element has no semantic click action") }
            control.snapshots.removeAll()
            guard !snapshot.app.runningApplication.isTerminated,
                  text(record.element, kAXTitleAttribute) == record.name,
                  text(record.element, kAXRoleAttribute) == record.role,
                  text(snapshot.window, kAXTitleAttribute) == snapshot.title,
                  actions(record.element).contains(action), belongs(record.element, to: snapshot.window),
                  attribute(record.element, kAXEnabledAttribute) as? Bool != false else {
                throw SDKDomainError("STALE_SNAPSHOT", "Application, window or element changed since observation")
            }
            try check(cancellation)
            let result = AXUIElementPerformAction(record.element, action as CFString)
            guard result == .success else {
                controlLock.withLock { uncertain = true }
                throw SDKDomainError("TARGET_UNAVAILABLE", "Accessibility action outcome is unknown", effect: "possible")
            }
            do {
                let updated = try observe(snapshot.options, control: control, cancellation: cancellation)
                return ["status": "completed", "observation": ["status": "available", "snapshot": updated]] as [String: Any]
            } catch {
                let reason = error as? SDKDomainError ?? SDKDomainError("TARGET_UNAVAILABLE", "Post-action observation failed")
                return ["status": "completed", "observation": ["status": "unavailable", "reason": ["code": reason.code, "message": reason.message]]] as [String: Any]
            }
        default:
            throw SDKDomainError("UNSUPPORTED_CAPABILITY", "Method is not connected to this desktop backend")
        }
    }

    private func observe(_ options: [String: Any], control: AppControl, cancellation: SDKCancellation) throws -> [String: Any] {
        let appID = control.appID
        control.snapshots.removeAll()
        try check(cancellation)
        guard let app = apps[appID], !app.runningApplication.isTerminated else { throw SDKDomainError("TARGET_UNAVAILABLE", "Application ID is not in this session") }
        guard AXIsProcessTrusted() else { throw SDKDomainError("PERMISSION_REQUIRED", "Accessibility permission is required for this app agent") }
        let application = AXUIElementCreateApplication(app.pid)
        AXUIElementSetMessagingTimeout(application, 0.5)
        let windows = attribute(application, kAXWindowsAttribute) as? [AXUIElement] ?? []
        guard let window = windows.first(where: { attribute($0, kAXMinimizedAttribute) as? Bool != true }) else {
            throw SDKDomainError("TARGET_UNAVAILABLE", "Application has no accessible, non-minimized window")
        }
        let title = text(window, kAXTitleAttribute)
        let maxNodes = min(try integer(options["maxTreeNodes"], fallback: 1200), 10000)
        let maxDepth = min(try integer(options["maxTreeDepth"], fallback: 64), 128)
        let maxText = options["textLimit"] as? String == "max" ? Int.max : try integer(options["textLimit"], fallback: 500)
        let snapshotID = UUID().uuidString
        var elements: [[String: Any]] = []
        var references: [String: Record] = [:]
        var truncated: Set<String> = []
        var queue: [(AXUIElement, String?, Int)] = [(window, nil, 0)]
        var cursor = 0
        func limited(_ string: String) -> String {
            if string.count > maxText { truncated.insert("text"); return String(string.prefix(maxText)) }
            return string
        }
        while cursor < queue.count {
            try check(cancellation)
            if elements.count >= maxNodes { truncated.insert("nodes"); break }
            let (element, parent, depth) = queue[cursor]; cursor += 1
            let id = "\(snapshotID):\(elements.count)"
            let name = text(element, kAXTitleAttribute)
            let role = text(element, kAXRoleAttribute)
            guard !role.isEmpty else { throw SDKDomainError("TARGET_UNAVAILABLE", "Accessibility element disappeared during observation") }
            let rawActions = actions(element)
            let action = [kAXPressAction as String, kAXConfirmAction as String, "AXOpen"].first(where: rawActions.contains)
            var record: [String: Any] = ["id": id, "role": role, "name": limited(name), "actions": action == nil ? [] : ["click"], "secondaryActions": []]
            if let parent { record["parentId"] = parent }
            if let value = attribute(element, kAXValueAttribute) as? String { record["value"] = limited(value) }
            elements.append(record)
            references[id] = Record(element: element, name: name, role: role, action: action)
            let children = attribute(element, kAXChildrenAttribute) as? [AXUIElement] ?? []
            if depth >= maxDepth {
                if !children.isEmpty { truncated.insert("depth") }
            } else {
                queue.append(contentsOf: children.map { ($0, id, depth + 1) })
            }
        }
        let screenshot = capture(app: app, window: window, title: title, cancellation: cancellation)
        try check(cancellation)
        control.snapshots[snapshotID] = Snapshot(app: app, window: window, title: title, elements: references, options: options)
        return ["id": snapshotID, "appSessionId": control.id, "app": ["id": appID, "name": app.name], "window": ["id": UUID().uuidString, "title": title],
                "tree": ["status": "available", "elements": elements, "truncated": truncated.sorted()], "screenshot": screenshot]
    }

    private func validateObservation(_ params: [String: Any]) throws {
        guard Set(params.keys).isSubset(of: ["appSessionId", "activation", "maxTreeNodes", "maxTreeDepth", "textLimit"]),
              let appSessionID = params["appSessionId"] as? String, !appSessionID.isEmpty else { throw SDKDomainError("INVALID_ARGUMENT", "Invalid observation parameters") }
        if let activation = params["activation"], !["never", "allow"].contains(activation as? String ?? "") { throw SDKDomainError("INVALID_ARGUMENT", "Invalid activation mode") }
        _ = try integer(params["maxTreeNodes"], fallback: 1200)
        _ = try integer(params["maxTreeDepth"], fallback: 64)
        if params["textLimit"] as? String != "max" { _ = try integer(params["textLimit"], fallback: 500) }
    }

    private func integer(_ value: Any?, fallback: Int) throws -> Int {
        guard let value else { return fallback }
        guard let number = value as? NSNumber, CFGetTypeID(number) != CFBooleanGetTypeID(), number.doubleValue >= 1,
              number.doubleValue <= 1e9, number.doubleValue == Double(number.intValue) else {
            throw SDKDomainError("INVALID_ARGUMENT", "Expected a positive integer")
        }
        return number.intValue
    }

    private func check(_ cancellation: SDKCancellation) throws {
        if cancellation.isCancelled { throw SDKDomainError("CANCELLED", "Request cancelled before further execution") }
    }
    private func attribute(_ element: AXUIElement, _ name: String) -> CFTypeRef? {
        var value: CFTypeRef?
        return AXUIElementCopyAttributeValue(element, name as CFString, &value) == .success ? value : nil
    }
    private func text(_ element: AXUIElement, _ name: String) -> String { attribute(element, name) as? String ?? "" }
    private func actions(_ element: AXUIElement) -> [String] {
        var values: CFArray?
        return AXUIElementCopyActionNames(element, &values) == .success ? values as? [String] ?? [] : []
    }
    private func belongs(_ element: AXUIElement, to window: AXUIElement) -> Bool {
        var parent = element
        for _ in 0...128 {
            if CFEqual(parent, window) { return true }
            guard let next = attribute(parent, kAXParentAttribute), CFGetTypeID(next) == AXUIElementGetTypeID() else { return false }
            parent = next as! AXUIElement
        }
        return false
    }

    private func capture(app: RunningAppDescriptor, window: AXUIElement, title: String, cancellation: SDKCancellation) -> [String: Any] {
        func unavailable(_ code: String, _ message: String) -> [String: Any] { ["status": "unavailable", "reason": ["code": code, "message": message]] }
        guard CGPreflightScreenCaptureAccess() else { return unavailable("PERMISSION_REQUIRED", "Screen Recording permission is required for this app agent") }
        guard let position = attribute(window, kAXPositionAttribute), let size = attribute(window, kAXSizeAttribute),
              CFGetTypeID(position) == AXValueGetTypeID(), CFGetTypeID(size) == AXValueGetTypeID() else { return unavailable("CAPTURE_FAILED", "Window bounds are unavailable") }
        var origin = CGPoint.zero
        var dimensions = CGSize.zero
        guard AXValueGetValue(position as! AXValue, .cgPoint, &origin), AXValueGetValue(size as! AXValue, .cgSize, &dimensions) else { return unavailable("CAPTURE_FAILED", "Window bounds are invalid") }
        let bounds = CGRect(origin: origin, size: dimensions)
        let pid = app.pid
        let box = SDKCaptureResult()
        let task = Task {
            do {
                let content = try await SCShareableContent.excludingDesktopWindows(false, onScreenWindowsOnly: true)
                let matches = content.windows.filter { candidate in
                    candidate.owningApplication?.processID == pid && candidate.title == title &&
                    abs(candidate.frame.minX - bounds.minX) < 2 && abs(candidate.frame.minY - bounds.minY) < 2 &&
                    abs(candidate.frame.width - bounds.width) < 2 && abs(candidate.frame.height - bounds.height) < 2
                }
                guard matches.count == 1, let target = matches.first else { throw SDKDomainError("CAPTURE_FAILED", "Cannot uniquely map the accessible window to a capture window") }
                try Task.checkCancellation()
                let configuration = SCStreamConfiguration()
                let scale = min(1, 1280 / max(target.frame.width, target.frame.height))
                configuration.width = max(1, Int(target.frame.width * scale))
                configuration.height = max(1, Int(target.frame.height * scale))
                configuration.showsCursor = false
                configuration.ignoreShadowsSingleWindow = true
                let image = try await SCScreenshotManager.captureImage(contentFilter: SCContentFilter(desktopIndependentWindow: target), configuration: configuration)
                guard let data = NSBitmapImageRep(cgImage: image).representation(using: .png, properties: [:]) else { throw SDKDomainError("CAPTURE_FAILED", "PNG encoding failed") }
                box.finish(["status": "available", "image": ["mimeType": "image/png", "width": image.width, "height": image.height, "dataBase64": data.base64EncodedString()]])
            } catch { box.finish(["status": "unavailable", "reason": ["code": "CAPTURE_FAILED", "message": "Window capture did not complete"]]) }
        }
        while box.done.wait(timeout: .now() + 0.02) != .success {
            if cancellation.isCancelled { task.cancel() }
        }
        return box.value
    }
}

private final class SDKCaptureResult: @unchecked Sendable {
    let done = DispatchSemaphore(value: 0)
    private(set) var value: [String: Any] = [:]
    func finish(_ result: [String: Any]) { value = result; done.signal() }
}
