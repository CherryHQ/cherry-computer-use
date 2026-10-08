import AppKit
import ApplicationServices
import Foundation

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
    fileprivate struct Snapshot {
        let state: AppSnapshot
        let options: [String: Any]
        let titles: [Int: String]
    }
    private let engine = ComputerUseService()
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
        let trusted = availability(AXIsProcessTrusted())
        var result = Dictionary(uniqueKeysWithValues: ["accessibility", "click", "performSecondaryAction", "scroll", "typeText", "pressKey", "setValue"].map { ($0, trusted) })
        result["screenshot"] = availability(CGPreflightScreenCaptureAccess())
        result["drag"] = availability(AXIsProcessTrusted() && CGPreflightScreenCaptureAccess())
        return result
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
            let action = try SDKAction(params)
            let control = try beginAppRequest(action.appSessionID, cancellation: cancellation)
            defer { endAppRequest(control) }
            guard let snapshot = control.snapshots[action.snapshotID] else {
                throw SDKDomainError("STALE_SNAPSHOT", "Snapshot is expired or belongs to another session")
            }
            control.snapshots.removeAll()
            try validateTarget(action, in: snapshot)
            try check(cancellation)
            do {
                try perform(action, on: snapshot.state)
            } catch let error as SDKDomainError {
                throw error
            } catch {
                throw SDKDomainError(error)
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

    private func perform(_ action: SDKAction, on state: AppSnapshot) throws {
        switch action.kind {
        case let .click(elementID, point, button, count):
            try engine.performClick(on: state, elementIndex: elementID, x: point.map { Double($0.x) }, y: point.map { Double($0.y) }, clickCount: count, mouseButton: button)
        case let .secondaryAction(elementID, actionID):
            try engine.performSecondaryAction(on: state, elementIndex: elementID, action: actionID)
        case let .scroll(elementID, direction, pages):
            try engine.performScroll(on: state, direction: direction, elementIndex: elementID ?? "0", pages: pages)
        case let .drag(from, to):
            try engine.performDrag(on: state, fromX: from.x, fromY: from.y, toX: to.x, toY: to.y)
        case let .typeText(text):
            try engine.performTypeText(on: state, text: text)
        case let .pressKey(key):
            try engine.performPressKey(on: state, key: key)
        case let .setValue(elementID, value):
            try engine.performSetValue(on: state, elementIndex: elementID, value: value)
        }
    }

    private func validateTarget(_ action: SDKAction, in snapshot: Snapshot) throws {
        let stale = SDKDomainError("STALE_SNAPSHOT", "Application, window or element changed since observation")
        guard !snapshot.state.app.runningApplication.isTerminated else { throw stale }
        if let elementID = action.elementID {
            guard let index = Int(elementID), let record = snapshot.state.elements[index] else { throw stale }
            if let element = record.element, let title = snapshot.titles[index] {
                guard text(element, kAXRoleAttribute) == record.role, text(element, kAXTitleAttribute) == title else { throw stale }
            }
        }
        if action.usesCoordinates {
            guard let id = snapshot.state.targetWindowID, let bounds = snapshot.state.windowBounds,
                  let info = (CGWindowListCopyWindowInfo([.optionIncludingWindow], id) as? [[String: Any]])?.first,
                  let raw = info[kCGWindowBounds as String] as? NSDictionary, let current = CGRect(dictionaryRepresentation: raw),
                  current.integral == bounds.integral else { throw stale }
        }
    }

    private func observe(_ options: [String: Any], control: AppControl, cancellation: SDKCancellation) throws -> [String: Any] {
        let appID = control.appID
        control.snapshots.removeAll()
        try check(cancellation)
        guard let app = apps[appID], !app.runningApplication.isTerminated else { throw SDKDomainError("TARGET_UNAVAILABLE", "Application ID is not in this session") }
        guard AXIsProcessTrusted() else { throw SDKDomainError("PERMISSION_REQUIRED", "Accessibility permission is required for this app agent") }
        let textLimit = options["textLimit"] as? String == "max" ? SnapshotTextLimit.max : SnapshotTextLimit(maxCount: try integer(options["textLimit"], fallback: defaultTextLimit))
        let limits = AccessibilityTreeLimits(maxNodeCount: try integer(options["maxTreeNodes"], fallback: AccessibilityTreeLimits.defaultMaxNodeCount), maxDepth: try integer(options["maxTreeDepth"], fallback: AccessibilityTreeLimits.defaultMaxDepth))
        let state: AppSnapshot
        do {
            state = try SnapshotBuilder.build(for: app, textLimit: textLimit, treeLimits: limits, recoveryPolicy: options["activation"] as? String == "allow" ? .allowActivation : .readOnly)
        } catch {
            throw SDKDomainError(error)
        }
        try check(cancellation)
        let snapshotID = UUID().uuidString
        let image = state.screenshotPNGData.flatMap { data -> (Data, Int, Int)? in
            guard let source = CGImageSourceCreateWithData(data as CFData, nil),
                  let properties = CGImageSourceCopyPropertiesAtIndex(source, 0, nil) as? [CFString: Any],
                  let width = properties[kCGImagePropertyPixelWidth] as? Int, let height = properties[kCGImagePropertyPixelHeight] as? Int else { return nil }
            return (data, width, height)
        }
        let scale = screenshotPixelScale(screenshotPixelSize: image.map { CGSize(width: $0.1, height: $0.2) }, windowBounds: state.windowBounds)
        var titles: [Int: String] = [:]
        let elements = state.elements.keys.sorted().map { index -> [String: Any] in
            let record = state.elements[index]!
            if let element = record.element, !record.isSyntheticText { titles[index] = text(element, kAXTitleAttribute) }
            var actions: [String] = []
            if record.localFrame != nil || !record.rawActions.isEmpty { actions.append("click") }
            if record.localFrame != nil { actions.append("scroll") }
            if ["AXTextField", "AXTextArea", "AXTextView", "AXComboBox"].contains(record.role ?? "") { actions.append("setValue") }
            if !record.prettyActions.isEmpty { actions.append("performSecondaryAction") }
            var element: [String: Any] = [
                "id": String(index), "role": record.role ?? (record.isSyntheticText ? "text" : "AXUnknown"), "name": record.name ?? "",
                "actions": actions, "secondaryActions": record.prettyActions.map { ["id": $0, "label": $0] },
            ]
            if let parent = record.parentIndex { element["parentId"] = String(parent) }
            if let value = record.value { element["value"] = value }
            if let frame = record.localFrame {
                element["bounds"] = ["x": frame.minX * scale.width, "y": frame.minY * scale.height, "width": frame.width * scale.width, "height": frame.height * scale.height]
            }
            return element
        }
        let screenshot: [String: Any]
        if let image {
            screenshot = ["status": "available", "image": ["mimeType": "image/png", "width": image.1, "height": image.2, "dataBase64": image.0.base64EncodedString()]]
        } else {
            let reason = CGPreflightScreenCaptureAccess()
                ? ["code": "CAPTURE_FAILED", "message": "Window capture is unavailable"]
                : ["code": "PERMISSION_REQUIRED", "message": "Screen Recording permission is required for this app agent"]
            screenshot = ["status": "unavailable", "reason": reason]
        }
        control.snapshots[snapshotID] = Snapshot(state: state, options: options, titles: titles)
        return ["id": snapshotID, "appSessionId": control.id, "app": ["id": appID, "name": app.name],
                "window": ["id": state.targetWindowID.map(String.init) ?? snapshotID, "title": state.windowTitle ?? ""],
                "tree": ["status": "available", "elements": elements, "text": state.renderedText(style: .fullState), "truncated": state.treeTruncation.sorted()],
                "screenshot": screenshot]
    }

    private func validateObservation(_ params: [String: Any]) throws {
        guard Set(params.keys).isSubset(of: ["appSessionId", "activation", "maxTreeNodes", "maxTreeDepth", "textLimit"]),
              let appSessionID = params["appSessionId"] as? String, !appSessionID.isEmpty else { throw SDKDomainError("INVALID_ARGUMENT", "Invalid observation parameters") }
        if let activation = params["activation"], !["never", "allow"].contains(activation as? String ?? "") { throw SDKDomainError("INVALID_ARGUMENT", "Invalid activation mode") }
        _ = try integer(params["maxTreeNodes"], fallback: 1200)
        _ = try integer(params["maxTreeDepth"], fallback: 64)
        if params["textLimit"] as? String != "max" { _ = try integer(params["textLimit"], fallback: 500) }
    }

    private func check(_ cancellation: SDKCancellation) throws {
        if cancellation.isCancelled { throw SDKDomainError("CANCELLED", "Request cancelled before further execution") }
    }
    private func text(_ element: AXUIElement, _ name: String) -> String {
        var value: CFTypeRef?
        return AXUIElementCopyAttributeValue(element, name as CFString, &value) == .success ? value as? String ?? "" : ""
    }
}

private func integer(_ value: Any?, fallback: Int) throws -> Int {
    guard let value else { return fallback }
    guard let number = value as? NSNumber, CFGetTypeID(number) != CFBooleanGetTypeID(), number.doubleValue >= 1,
          number.doubleValue <= 1e9, number.doubleValue == Double(number.intValue) else {
        throw SDKDomainError("INVALID_ARGUMENT", "Expected a positive integer")
    }
    return number.intValue
}

private extension SDKDomainError {
    /// Engine failures surface after input may have been dispatched, so their effect is unknown.
    init(_ error: Error) {
        switch error as? ComputerUseError {
        case .invalidArguments(let message): self.init("INVALID_ARGUMENT", message)
        case .permissionDenied(let message): self.init("PERMISSION_REQUIRED", message)
        case .appNotFound(let message): self.init("TARGET_UNAVAILABLE", message)
        default: self.init("TARGET_UNAVAILABLE", error.localizedDescription, effect: "possible")
        }
    }
}

private struct SDKAction {
    enum Kind {
        case click(elementID: String?, point: CGPoint?, button: String, count: Int)
        case secondaryAction(elementID: String, actionID: String)
        case scroll(elementID: String?, direction: String, pages: Double)
        case drag(from: CGPoint, to: CGPoint)
        case typeText(String)
        case pressKey(String)
        case setValue(elementID: String, value: String)
    }
    let appSessionID: String
    let snapshotID: String
    let kind: Kind

    var elementID: String? {
        switch kind {
        case let .click(id, _, _, _): return id
        case let .secondaryAction(id, _), let .setValue(id, _): return id
        case let .scroll(id, _, _): return id
        case .drag, .typeText, .pressKey: return nil
        }
    }
    var usesCoordinates: Bool {
        switch kind {
        case let .click(id, _, _, _): return id == nil
        case .drag: return true
        default: return false
        }
    }

    init(_ params: [String: Any]) throws {
        let invalid = SDKDomainError("INVALID_ARGUMENT", "Invalid action parameters")
        func string(_ key: String) throws -> String {
            guard let value = params[key] as? String, !value.isEmpty else { throw invalid }
            return value
        }
        func number(_ value: Any?) throws -> Double {
            guard let number = value as? NSNumber, CFGetTypeID(number) != CFBooleanGetTypeID(), number.doubleValue.isFinite, number.doubleValue >= 0 else { throw invalid }
            return number.doubleValue
        }
        func point(_ key: String) throws -> CGPoint {
            guard let value = params[key] as? [String: Any], Set(value.keys) == ["x", "y"] else { throw invalid }
            return CGPoint(x: try number(value["x"]), y: try number(value["y"]))
        }
        let type = try string("type")
        let common: Set<String> = ["type", "appSessionId", "snapshotId", "allowGlobalInput"]
        let allowed: Set<String>
        switch type {
        case "click":
            allowed = ["elementId", "x", "y", "button", "count"]
            let elementID = params["elementId"] == nil ? nil : try string("elementId")
            let hasPoint = params["x"] != nil || params["y"] != nil
            guard (elementID == nil) == hasPoint else { throw invalid }
            let button = params["button"] == nil ? "left" : try string("button")
            let count = try integer(params["count"], fallback: 1)
            guard ["left", "right", "middle"].contains(button), count <= 3 else { throw invalid }
            kind = .click(elementID: elementID, point: hasPoint ? CGPoint(x: try number(params["x"]), y: try number(params["y"])) : nil, button: button, count: count)
        case "performSecondaryAction":
            allowed = ["elementId", "actionId"]
            kind = .secondaryAction(elementID: try string("elementId"), actionID: try string("actionId"))
        case "scroll":
            allowed = ["elementId", "direction", "pages"]
            let direction = try string("direction")
            let pages = try number(params["pages"])
            guard ["up", "down", "left", "right"].contains(direction), pages > 0 else { throw invalid }
            kind = .scroll(elementID: params["elementId"] == nil ? nil : try string("elementId"), direction: direction, pages: pages)
        case "drag":
            allowed = ["from", "to"]
            kind = .drag(from: try point("from"), to: try point("to"))
        case "typeText":
            allowed = ["text"]
            kind = .typeText(try string("text"))
        case "pressKey":
            allowed = ["key"]
            kind = .pressKey(try string("key"))
        case "setValue":
            allowed = ["elementId", "value"]
            guard let value = params["value"] as? String else { throw invalid }
            kind = .setValue(elementID: try string("elementId"), value: value)
        default:
            throw SDKDomainError("UNSUPPORTED_CAPABILITY", "Unknown action type")
        }
        guard Set(params.keys).isSubset(of: common.union(allowed)) else { throw invalid }
        if let allow = params["allowGlobalInput"], !(allow is NSNumber && CFGetTypeID(allow as CFTypeRef) == CFBooleanGetTypeID()) { throw invalid }
        appSessionID = try string("appSessionId")
        snapshotID = try string("snapshotId")
    }
}
