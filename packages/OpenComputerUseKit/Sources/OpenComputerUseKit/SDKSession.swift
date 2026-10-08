import Foundation

public final class SDKCancellation: @unchecked Sendable {
    private let lock = NSLock()
    private var cancelled = false
    public var isCancelled: Bool { lock.withLock { cancelled } }
    public func cancel() { lock.withLock { cancelled = true } }
}

public final class SDKSession: @unchecked Sendable {
    private let sessionID: String
    private let version: String
    private let transport: SDKTransport
    private let permissions: @Sendable (SDKCancellation) throws -> [[String: String]]
    private let permissionRequest: @Sendable ([String], SDKCancellation) throws -> [[String: String]]
    private let cleanup: @Sendable () throws -> Void
    private let desktop: (any SDKDesktopBackend)?
    private let lock = NSLock()
    private let execution = DispatchQueue(label: "computer-use.sdk.execution")
    private let output = DispatchQueue(label: "computer-use.sdk.output")
    private let finished = DispatchSemaphore(value: 0)
    private let controlRequests = DispatchGroup()
    private var initialized = false
    private var pending: [String: SDKCancellation] = [:]
    private var queuedResponses = 0
    private var closing = false
    private var complete = false
    private var failure: SDKProtocolError?

    public init(
        sessionID: String, version: String, transport: SDKTransport,
        permissions: (@Sendable (SDKCancellation) throws -> [[String: String]])? = nil,
        permissionRequest: (@Sendable ([String], SDKCancellation) throws -> [[String: String]])? = nil,
        cleanup: @escaping @Sendable () throws -> Void = {},
        desktop: (any SDKDesktopBackend)? = nil
    ) {
        self.sessionID = sessionID
        self.version = version
        self.transport = transport
        self.permissions = permissions ?? { _ in SDKPermissions.status() }
        self.permissionRequest = permissionRequest ?? { try SDKPermissions.request($0, cancellation: $1) }
        self.cleanup = cleanup
        self.desktop = desktop
    }

    public func run() throws {
        do {
            while let message = try transport.read() {
                try receive(message)
                if lock.withLock({ closing }) { break }
            }
            close(id: nil)
        } catch {
            fail(String(describing: error))
        }
        finished.wait()
        if let error = lock.withLock({ failure }) { throw error }
    }

    private func receive(_ message: SDKMessage) throws {
        let value = message.value
        guard Set(value.keys).isSubset(of: ["jsonrpc", "id", "method", "params"]),
              value["jsonrpc"] as? String == "2.0", let method = value["method"] as? String, !method.isEmpty else {
            throw SDKProtocolError("Invalid request envelope")
        }
        if value["id"] == nil {
            guard method == "$/cancelRequest", let params = value["params"] as? [String: Any],
                  Set(params.keys) == ["id"], let id = params["id"] as? String, !id.isEmpty else {
                throw SDKProtocolError("Invalid notification")
            }
            lock.withLock { pending[id]?.cancel() }
            return
        }
        guard let id = value["id"] as? String, !id.isEmpty else { throw SDKProtocolError("Invalid request ID") }
        guard !lock.withLock({ pending[id] != nil }) else { throw SDKProtocolError("Duplicate active request ID") }
        let params = value["params"] as? [String: Any]
        if method == "initialize" {
            guard !initialized, let params, Set(params.keys) == ["protocolVersion", "sessionId"],
                  let protocolVersion = params["protocolVersion"] as? NSNumber,
                  CFGetTypeID(protocolVersion) != CFBooleanGetTypeID(), protocolVersion == 2,
                  params["sessionId"] as? String == sessionID else {
                send(error(id, "PROTOCOL_MISMATCH", "Protocol version or private session does not match"))
                return
            }
            initialized = true
            send(result(id, ["protocolVersion": 2, "runtimeVersion": version, "sessionId": sessionID, "ownership": "private"]))
            return
        }
        if method == "shutdown" {
            guard let params, Set(params.keys) == ["sessionId"], params["sessionId"] as? String == sessionID else {
                send(error(id, "INVALID_ARGUMENT", "Shutdown session does not match"))
                return
            }
            close(id: id)
            return
        }
        guard initialized else {
            send(error(id, "PROTOCOL_ERROR", "Initialize the runtime first"))
            return
        }
        let cancellation = SDKCancellation()
        try lock.withLock {
            let limit = method == "stopAppSession" || method == "listAppSessions" ? 128 : 64
            guard pending.count < limit else { throw SDKProtocolError("Request queue full") }
            pending[id] = cancellation
        }
        if let desktop, method == "listAppSessions" || method == "stopAppSession" {
            do {
                guard let params else { throw SDKDomainError("INVALID_ARGUMENT", "Expected object parameters") }
                if method == "listAppSessions" {
                    guard params.isEmpty else { throw SDKDomainError("INVALID_ARGUMENT", "Expected empty parameters") }
                    send(result(id, desktop.listAppSessions()))
                    _ = lock.withLock { pending.removeValue(forKey: id) }
                } else {
                    let finish = try desktop.prepareAppStop(params)
                    controlRequests.enter()
                    DispatchQueue.global().async {
                        defer {
                            _ = self.lock.withLock { self.pending.removeValue(forKey: id) }
                            self.controlRequests.leave()
                        }
                        do { self.send(self.result(id, try finish().value)) }
                        catch let error as SDKDomainError { self.send(self.error(id, error.code, error.message, effect: error.effect)) }
                        catch { self.send(self.error(id, "CLEANUP_FAILED", "Application cleanup failed", effect: "possible")) }
                    }
                }
            } catch let error as SDKDomainError {
                send(self.error(id, error.code, error.message, effect: error.effect))
                _ = lock.withLock { pending.removeValue(forKey: id) }
            }
            return
        }
        execution.async {
            let response: SDKMessage
            if cancellation.isCancelled {
                response = self.error(id, "CANCELLED", "Request cancelled before execution")
            } else if method == "getCapabilities" || method == "getPermissionStatus" {
                if (message.value["params"] as? [String: Any])?.isEmpty != true {
                    response = self.error(id, "INVALID_ARGUMENT", "Expected empty parameters")
                } else if method == "getCapabilities" {
                    let available = self.desktop?.capabilities() ?? [:]
                    let capabilities = ["accessibility", "screenshot", "click", "performSecondaryAction", "scroll", "drag", "typeText", "pressKey", "setValue"].map { name in
                        ["name": name, "availability": available[name] ?? ["status": "unsupported", "reason": ["code": "UNSUPPORTED_CAPABILITY", "message": "Capability is not connected to SDK protocol"]]] as [String: Any]
                    }
                    response = self.result(id, ["platform": "darwin", "capabilities": capabilities])
                } else {
                    do {
                        let permissions = try self.permissions(cancellation)
                        response = cancellation.isCancelled
                            ? self.error(id, "CANCELLED", "Permission query cancelled")
                            : self.result(id, ["permissions": permissions])
                    } catch {
                        response = self.error(id, cancellation.isCancelled ? "CANCELLED" : "PERMISSION_REQUIRED", "Permission query failed")
                    }
                }
            } else if method == "requestPermissions" {
                do {
                    guard let params = message.value["params"] as? [String: Any], Set(params.keys) == ["ids"],
                          let ids = params["ids"] as? [String], !ids.isEmpty, ids.allSatisfy({ !$0.isEmpty }), Set(ids).count == ids.count else {
                        throw SDKDomainError("INVALID_ARGUMENT", "Expected unique permission IDs")
                    }
                    response = self.result(id, ["permissions": try self.permissionRequest(ids, cancellation)])
                } catch let error as SDKDomainError {
                    response = self.error(id, error.code, error.message, effect: error.effect)
                } catch {
                    response = self.error(id, "PERMISSION_REQUIRED", "Permission request failed", effect: "possible")
                }
            } else if let desktop = self.desktop {
                do {
                    guard let params = message.value["params"] as? [String: Any] else { throw SDKDomainError("INVALID_ARGUMENT", "Expected object parameters") }
                    response = self.result(id, try desktop.call(method, params: params, cancellation: cancellation))
                } catch let error as SDKDomainError {
                    response = self.error(id, error.code, error.message, effect: error.effect)
                } catch {
                    response = self.error(id, "TARGET_UNAVAILABLE", "Desktop request failed")
                }
            } else {
                response = self.error(id, "UNSUPPORTED_CAPABILITY", "Desktop methods are not connected to the SDK runtime yet")
            }
            self.send(response)
            _ = self.lock.withLock { self.pending.removeValue(forKey: id) }
        }
    }

    private func result(_ id: String, _ value: Any) -> SDKMessage {
        SDKMessage(["jsonrpc": "2.0", "id": id, "result": value])
    }

    private func error(_ id: String, _ code: String, _ message: String, effect: String = "none") -> SDKMessage {
        SDKMessage(["jsonrpc": "2.0", "id": id, "error": ["code": -32000, "message": message, "data": ["code": code, "effect": effect]]])
    }

    private func send(_ message: SDKMessage) {
        let accepted = lock.withLock {
            guard !complete, queuedResponses < 128 else { return false }
            queuedResponses += 1
            return true
        }
        guard accepted else { fail("Response queue full"); return }
        output.async {
            defer { self.lock.withLock { self.queuedResponses -= 1 } }
            do { try self.transport.write(message) }
            catch { self.fail("Protocol output failed") }
        }
    }

    private func fail(_ message: String) {
        lock.withLock { if failure == nil { failure = SDKProtocolError(message) } }
        close(id: nil)
    }

    private func close(id: String?) {
        let first = lock.withLock {
            if closing { return false }
            closing = true
            for token in pending.values { token.cancel() }
            return true
        }
        guard first else { return }
        transport.stopReading()
        DispatchQueue.global().asyncAfter(deadline: .now() + 1) {
            self.finish(SDKProtocolError("CLEANUP_FAILED: request or output did not settle"))
        }
        execution.async {
            do {
                guard self.controlRequests.wait(timeout: .now() + 1) == .success else {
                    throw SDKProtocolError("Application cleanup did not settle")
                }
                try self.desktop?.close()
                try self.cleanup()
                self.output.async {
                    do {
                        if let id, self.lock.withLock({ self.failure == nil && !self.complete }) {
                            try self.transport.write(self.result(id, ["sessionId": self.sessionID, "cleanup": "complete"]))
                        }
                        self.finish(nil)
                    } catch { self.finish(SDKProtocolError("Protocol shutdown output failed")) }
                }
            } catch { self.finish(SDKProtocolError("CLEANUP_FAILED: backend cleanup failed")) }
        }
    }

    private func finish(_ error: SDKProtocolError?) {
        let first = lock.withLock {
            if complete { return false }
            complete = true
            if failure == nil { failure = error }
            return true
        }
        if first { transport.stop(); finished.signal() }
    }
}
