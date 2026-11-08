import Foundation

/// Delivers text and key input to Chromium and Electron pages over the Chrome DevTools Protocol.
///
/// This mirrors `packages/runtime-go/webcdp` for the macOS Swift runtime: the same discovery rules
/// (`--remote-debugging-port` or `DevToolsActivePort` inside `--user-data-dir`), the same page-target
/// selection, and the same `Input.insertText` / `Input.dispatchKeyEvent` payloads. Every failure
/// returns false so the caller keeps using Accessibility, never "delivered but ignored".
enum WebCDPInput {
    struct Target: Equatable {
        let title: String
        let webSocketURL: String
    }

    /// A page target resolved while observing a window, so input never has to discover it.
    struct Endpoint: Equatable {
        let webSocketURL: String
        let title: String
    }

    enum Outcome {
        case delivered
        /// No DevTools endpoint for this window: safe to use Accessibility.
        case unavailable
        /// The page accepted the call but nothing changed. Retrying elsewhere would apply it twice.
        case notDelivered
    }

    struct KeyDescriptor: Equatable {
        let key: String
        let code: String
        let virtualKey: Int
        let text: String?
        let modifiers: Int
    }

    struct CDPFailure: Error {
        let message: String
    }

    static var isEnabled: Bool {
        let value = (ProcessInfo.processInfo.environment["OPEN_COMPUTER_USE_DISABLE_CDP"] ?? "")
            .trimmingCharacters(in: .whitespacesAndNewlines)
            .lowercased()
        return !["1", "true", "yes", "on"].contains(value)
    }

    // MARK: - Input

    /// Resolves the page channel for a window. Called while observing, never while typing.
    static func resolveEndpoint(pid: pid_t, windowTitle: String?) -> Endpoint? {
        guard isEnabled, let port = port(forPID: pid) else { return nil }
        guard let target = selectTarget(pageTargets(port: port), windowTitle: windowTitle) else { return nil }
        return Endpoint(webSocketURL: target.webSocketURL, title: target.title)
    }

    static func insertText(_ text: String, endpoint: Endpoint) -> Outcome {
        guard isEnabled, !text.isEmpty else { return .unavailable }
        let focusParams = objectJSON([
            "expression": focusExpression,
            "returnByValue": true,
            "awaitPromise": true,
        ])
        let insertParams = objectJSON(["text": text])
        let valueParams = objectJSON([
            "expression": activeValueExpression,
            "returnByValue": true,
            "awaitPromise": true,
        ])
        do {
            let reply = try runSync { () -> (focus: Data, value: Data) in
                let session = try await openSession(endpoint.webSocketURL)
                defer { session.task.cancel(with: .normalClosure, reason: nil) }
                let focus = try await session.call(method: "Runtime.evaluate", paramsJSON: focusParams)
                _ = try await session.call(method: "Input.insertText", paramsJSON: insertParams)
                let value = try await session.call(method: "Runtime.evaluate", paramsJSON: valueParams)
                return (focus, value)
            }
            guard let before = decodeFocus(reply.focus), before.focused else { return .unavailable }
            guard let after = decodeString(reply.value) else { return .unavailable }
            return after.count > before.value.count ? .delivered : .notDelivered
        } catch {
            return .unavailable
        }
    }

    static func pressKey(_ key: String, endpoint: Endpoint) -> Outcome {
        guard isEnabled, let descriptor = descriptor(forKey: key) else { return .unavailable }
        let down = keyEventJSON(descriptor, type: "keyDown")
        let up = keyEventJSON(descriptor, type: "keyUp")
        do {
            try runSync { () -> Void in
                let session = try await openSession(endpoint.webSocketURL)
                defer { session.task.cancel(with: .normalClosure, reason: nil) }
                _ = try await session.call(method: "Input.dispatchKeyEvent", paramsJSON: down)
                _ = try await session.call(method: "Input.dispatchKeyEvent", paramsJSON: up)
            }
            return .delivered
        } catch {
            return .unavailable
        }
    }

    // MARK: - Discovery

    static func port(forPID pid: pid_t) -> Int? {
        guard let line = commandLine(pid: pid) else { return nil }
        if let port = portFromCommandLine(line) {
            return port
        }
        guard let directory = userDataDir(fromCommandLine: line) else { return nil }
        let path = (directory as NSString).appendingPathComponent("DevToolsActivePort")
        return portFromActivePortFile(path)
    }

    static func commandLine(pid: pid_t) -> String? {
        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/bin/ps")
        process.arguments = ["-o", "command=", "-p", String(pid)]
        let pipe = Pipe()
        process.standardOutput = pipe
        process.standardError = FileHandle.nullDevice
        do {
            try process.run()
        } catch {
            return nil
        }
        let data = pipe.fileHandleForReading.readDataToEndOfFile()
        process.waitUntilExit()
        guard process.terminationStatus == 0 else { return nil }
        return String(data: data, encoding: .utf8)?.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    static func portFromCommandLine(_ line: String) -> Int? {
        let prefix = "--remote-debugging-port="
        guard let range = line.range(of: prefix) else { return nil }
        let rest = line[range.upperBound...]
        let digits = rest.prefix { $0.isNumber }
        guard let port = Int(digits), port > 0, port <= 65535 else { return nil }
        return port
    }

    static func userDataDir(fromCommandLine line: String) -> String? {
        let prefix = "--user-data-dir="
        guard let range = line.range(of: prefix) else { return nil }
        let rest = line[range.upperBound...]
        if rest.hasPrefix("\"") {
            let inner = rest.dropFirst()
            guard let closing = inner.firstIndex(of: "\"") else { return nil }
            let value = String(inner[inner.startIndex..<closing])
            return value.isEmpty ? nil : value
        }
        let value = rest.prefix { $0 != " " && $0 != "\t" }
        return value.isEmpty ? nil : String(value)
    }

    static func portFromActivePortFile(_ path: String) -> Int? {
        guard let content = try? String(contentsOfFile: path, encoding: .utf8) else { return nil }
        let firstLine = content.split(whereSeparator: { $0 == "\n" || $0 == "\r" }).first ?? ""
        guard let port = Int(firstLine.trimmingCharacters(in: .whitespaces)), port > 0, port <= 65535 else { return nil }
        return port
    }

    static func pageTargets(port: Int) -> [Target] {
        guard let url = URL(string: "http://127.0.0.1:\(port)/json/list") else { return [] }
        guard let data = try? fetch(url),
              let entries = try? JSONSerialization.jsonObject(with: data) as? [[String: Any]] else { return [] }
        return entries.compactMap { entry in
            guard entry["type"] as? String == "page",
                  let webSocketURL = entry["webSocketDebuggerUrl"] as? String,
                  !webSocketURL.isEmpty else { return nil }
            return Target(title: entry["title"] as? String ?? "", webSocketURL: webSocketURL)
        }
    }

    /// Exact title first, then a prefix match in either direction (browsers append the profile name).
    static func selectTarget(_ targets: [Target], windowTitle: String?) -> Target? {
        guard !targets.isEmpty else { return nil }
        if let windowTitle, !windowTitle.isEmpty {
            if let exact = targets.first(where: { $0.title == windowTitle }) { return exact }
            if let prefix = targets.first(where: {
                !$0.title.isEmpty && (windowTitle.hasPrefix($0.title) || $0.title.hasPrefix(windowTitle))
            }) { return prefix }
        }
        return targets.first
    }

    // MARK: - Key table

    static func descriptor(forKey key: String) -> KeyDescriptor? {
        guard let chord = splitChord(key) else { return nil }
        let main = chord.main
        var descriptor: KeyDescriptor
        if let named = namedKeys[main.lowercased()] {
            descriptor = named
        } else if main.count == 1, let scalar = main.uppercased().unicodeScalars.first {
            descriptor = KeyDescriptor(
                key: main,
                code: "Key" + main.uppercased(),
                virtualKey: Int(scalar.value),
                text: main,
                modifiers: 0
            )
        } else if let index = functionKeyIndex(main) {
            descriptor = KeyDescriptor(key: main, code: main, virtualKey: 0x70 + index - 1, text: nil, modifiers: 0)
        } else {
            return nil
        }
        var modifiers = descriptor.modifiers
        if chord.alt { modifiers |= 1 }
        if chord.control { modifiers |= 2 }
        if chord.meta { modifiers |= 4 }
        if chord.shift { modifiers |= 8 }
        return KeyDescriptor(
            key: descriptor.key,
            code: descriptor.code,
            virtualKey: descriptor.virtualKey,
            text: descriptor.text,
            modifiers: modifiers
        )
    }

    private static let namedKeys: [String: KeyDescriptor] = [
        "return": KeyDescriptor(key: "Enter", code: "Enter", virtualKey: 13, text: "\r", modifiers: 0),
        "enter": KeyDescriptor(key: "Enter", code: "Enter", virtualKey: 13, text: "\r", modifiers: 0),
        "tab": KeyDescriptor(key: "Tab", code: "Tab", virtualKey: 9, text: nil, modifiers: 0),
        "escape": KeyDescriptor(key: "Escape", code: "Escape", virtualKey: 27, text: nil, modifiers: 0),
        "esc": KeyDescriptor(key: "Escape", code: "Escape", virtualKey: 27, text: nil, modifiers: 0),
        "backspace": KeyDescriptor(key: "Backspace", code: "Backspace", virtualKey: 8, text: nil, modifiers: 0),
        "delete": KeyDescriptor(key: "Delete", code: "Delete", virtualKey: 46, text: nil, modifiers: 0),
        "space": KeyDescriptor(key: " ", code: "Space", virtualKey: 32, text: " ", modifiers: 0),
        "left": KeyDescriptor(key: "ArrowLeft", code: "ArrowLeft", virtualKey: 37, text: nil, modifiers: 0),
        "up": KeyDescriptor(key: "ArrowUp", code: "ArrowUp", virtualKey: 38, text: nil, modifiers: 0),
        "right": KeyDescriptor(key: "ArrowRight", code: "ArrowRight", virtualKey: 39, text: nil, modifiers: 0),
        "down": KeyDescriptor(key: "ArrowDown", code: "ArrowDown", virtualKey: 40, text: nil, modifiers: 0),
        "home": KeyDescriptor(key: "Home", code: "Home", virtualKey: 36, text: nil, modifiers: 0),
        "end": KeyDescriptor(key: "End", code: "End", virtualKey: 35, text: nil, modifiers: 0),
        "page_up": KeyDescriptor(key: "PageUp", code: "PageUp", virtualKey: 33, text: nil, modifiers: 0),
        "prior": KeyDescriptor(key: "PageUp", code: "PageUp", virtualKey: 33, text: nil, modifiers: 0),
        "page_down": KeyDescriptor(key: "PageDown", code: "PageDown", virtualKey: 34, text: nil, modifiers: 0),
        "next": KeyDescriptor(key: "PageDown", code: "PageDown", virtualKey: 34, text: nil, modifiers: 0),
        "insert": KeyDescriptor(key: "Insert", code: "Insert", virtualKey: 45, text: nil, modifiers: 0),
    ]

    private struct Chord {
        let main: String
        let shift: Bool
        let control: Bool
        let alt: Bool
        let meta: Bool
    }

    /// Splits "ctrl+shift+s" into its key and modifiers. A trailing '+' is the key itself.
    private static func splitChord(_ key: String) -> Chord? {
        var main = key
        var shift = false
        var control = false
        var alt = false
        var meta = false
        while let index = main.firstIndex(of: "+"), index != main.startIndex, main.index(after: index) != main.endIndex {
            switch main[main.startIndex..<index].lowercased() {
            case "ctrl", "control": control = true
            case "shift": shift = true
            case "alt": alt = true
            case "super", "win", "cmd", "meta": meta = true
            default: return nil
            }
            main = String(main[main.index(after: index)...])
        }
        guard !main.isEmpty else { return nil }
        return Chord(main: main, shift: shift, control: control, alt: alt, meta: meta)
    }

    private static func functionKeyIndex(_ key: String) -> Int? {
        guard key.count >= 2, key.count <= 3, key.lowercased().hasPrefix("f") else { return nil }
        guard let index = Int(key.dropFirst()), index >= 1, index <= 12 else { return nil }
        return index
    }

    // MARK: - Transport

    final class Session: @unchecked Sendable {
        let task: URLSessionWebSocketTask
        private var nextIdentifier = 0

        init(task: URLSessionWebSocketTask) {
            self.task = task
        }

        func call(method: String, paramsJSON: String) async throws -> Data {
            nextIdentifier += 1
            let identifier = nextIdentifier
            let escapedMethod = objectJSON(method)
            let payload = "{\"id\":\(identifier),\"method\":\(escapedMethod),\"params\":\(paramsJSON)}"
            try await task.send(.string(payload))
            while true {
                let message = try await task.receive()
                let data: Data
                switch message {
                case let .string(text):
                    data = Data(text.utf8)
                case let .data(value):
                    data = value
                @unknown default:
                    continue
                }
                guard let envelope = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else { continue }
                guard let responseID = envelope["id"] as? Int, responseID == identifier else { continue }
                if let error = envelope["error"] as? [String: Any] {
                    throw CDPFailure(message: error["message"] as? String ?? "DevTools error")
                }
                let result = envelope["result"] ?? [String: Any]()
                return (try? JSONSerialization.data(withJSONObject: result)) ?? Data("{}".utf8)
            }
        }
    }

    private static func openSession(_ webSocketURL: String) async throws -> Session {
        guard let url = URL(string: webSocketURL) else { throw CDPFailure(message: "invalid endpoint") }
        let task = URLSession.shared.webSocketTask(with: url)
        task.resume()
        return Session(task: task)
    }

    private static func fetch(_ url: URL) throws -> Data {
        try runSync {
            let (data, _) = try await URLSession.shared.data(from: url)
            return data
        }
    }

    private final class Box<Value>: @unchecked Sendable {
        private let lock = NSLock()
        private var value: Value?

        func set(_ newValue: Value) {
            lock.lock()
            value = newValue
            lock.unlock()
        }

        func get() -> Value? {
            lock.lock()
            defer { lock.unlock() }
            return value
        }
    }

    /// The SDK action path is synchronous; bridge to the async transport with a bounded wait.
    private static func runSync<T: Sendable>(timeout: TimeInterval = 15, _ body: @escaping @Sendable () async throws -> T) throws -> T {
        let semaphore = DispatchSemaphore(value: 0)
        let box = Box<Result<T, Error>>()
        Task.detached {
            do {
                box.set(.success(try await body()))
            } catch {
                box.set(.failure(error))
            }
            semaphore.signal()
        }
        guard semaphore.wait(timeout: .now() + timeout) == .success else {
            throw CDPFailure(message: "DevTools call timed out")
        }
        guard let result = box.get() else { throw CDPFailure(message: "DevTools call produced no result") }
        return try result.get()
    }

    // MARK: - Payloads

    private static let focusExpression = """
    (function () {
      function editable(element) {
        if (!element) return false;
        if (element.isContentEditable) return true;
        return /^(INPUT|TEXTAREA)$/.test(element.tagName) && !element.disabled && !element.readOnly;
      }
      var active = document.activeElement;
      if (editable(active)) {
        return { focused: true, value: active.value == null ? '' : String(active.value) };
      }
      var selector = 'input:not([type=hidden]):not([type=button]):not([type=submit]):not([type=checkbox]):not([type=radio]),textarea,[contenteditable=true]';
      var candidates = Array.prototype.slice.call(document.querySelectorAll(selector));
      for (var index = 0; index < candidates.length; index++) {
        var element = candidates[index];
        var rect = element.getBoundingClientRect();
        if (rect.width <= 0 || rect.height <= 0) continue;
        element.focus();
        return { focused: true, value: element.value == null ? '' : String(element.value) };
      }
      return { focused: false };
    })()
    """

    private static let activeValueExpression = """
    document.activeElement
      ? String(document.activeElement.value == null ? document.activeElement.textContent : document.activeElement.value)
      : ''
    """

    private static func keyEventJSON(_ descriptor: KeyDescriptor, type: String) -> String {
        var payload: [String: Any] = [
            "type": type,
            "key": descriptor.key,
            "code": descriptor.code,
            "windowsVirtualKeyCode": descriptor.virtualKey,
            "nativeVirtualKeyCode": descriptor.virtualKey,
            "modifiers": descriptor.modifiers,
        ]
        if let text = descriptor.text {
            payload["text"] = text
            payload["unmodifiedText"] = text
        }
        return objectJSON(payload)
    }

    private static func objectJSON(_ value: Any) -> String {
        guard let data = try? JSONSerialization.data(withJSONObject: value, options: [.fragmentsAllowed]),
              let text = String(data: data, encoding: .utf8) else { return "{}" }
        return text
    }

    private static func decodeFocus(_ data: Data) -> (focused: Bool, value: String)? {
        guard let envelope = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let result = envelope["result"] as? [String: Any],
              let value = result["value"] as? [String: Any] else { return nil }
        return (value["focused"] as? Bool ?? false, value["value"] as? String ?? "")
    }

    private static func decodeString(_ data: Data) -> String? {
        guard let envelope = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let result = envelope["result"] as? [String: Any] else { return nil }
        return result["value"] as? String
    }
}
