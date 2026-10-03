import Foundation
import HTTPTypes
import OpenAPIRuntime

/// A transport that routes each request through a closure and records every call. Unlike `StubTransport`
/// (one canned response), this sees the method, path, query and body, and may suspend (see `Gate`).
actor RoutingTransport: ClientTransport {
    struct Call: Sendable, Equatable {
        let method: String
        /// Path and query, e.g. `/meals?limit=100&cursor=c2`.
        let path: String
        let body: String
        /// `"GET /meals"`: the method and the path without its query.
        var route: String { method + " " + (path.split(separator: "?", maxSplits: 1).first.map(String.init) ?? path) }
    }

    typealias Route = @Sendable (Call) async throws -> (status: Int, body: String)

    private(set) var calls: [Call] = []
    private let route: Route

    init(_ route: @escaping Route) { self.route = route }

    func calls(_ route: String) -> [Call] { calls.filter { $0.route == route } }

    func send(_ request: HTTPRequest, body: HTTPBody?, baseURL: URL, operationID: String) async throws -> (HTTPResponse, HTTPBody?) {
        var text = ""
        if let body { text = try await String(collecting: body, upTo: 1_048_576) }
        // The generated client pretty-prints JSON; record it compact (sorted keys) so tests can match `"name":"New"`.
        if let data = text.data(using: .utf8), let object = try? JSONSerialization.jsonObject(with: data),
           let compact = try? JSONSerialization.data(withJSONObject: object, options: [.sortedKeys]) {
            text = String(decoding: compact, as: UTF8.self)
        }
        let call = Call(method: request.method.rawValue, path: request.path ?? "", body: text)
        calls.append(call)
        let (status, json) = try await route(call)
        // Every error response is RFC 9457 `application/problem+json`, as the real API sends.
        let type = status >= 400 ? "application/problem+json" : "application/json"
        return (HTTPResponse(status: .init(code: status), headerFields: [.contentType: type]), HTTPBody(json))
    }
}

/// Mutable state a `@Sendable` route closure can read and a test can change.
final class Locked<Value: Sendable>: @unchecked Sendable {
    private let lock = NSLock()
    private var stored: Value
    init(_ value: Value) { stored = value }
    var value: Value { lock.lock(); defer { lock.unlock() }; return stored }
    func set(_ value: Value) { lock.lock(); defer { lock.unlock() }; stored = value }
    func mutate(_ change: (inout Value) -> Void) { lock.lock(); defer { lock.unlock() }; change(&stored) }
}

/// Holds a route (or anything) until the test releases it.
actor Gate {
    private var isOpen = false
    private var waiters: [CheckedContinuation<Void, Never>] = []
    func wait() async {
        if isOpen { return }
        await withCheckedContinuation { waiters.append($0) }
    }
    func release() {
        isOpen = true
        for waiter in waiters { waiter.resume() }
        waiters = []
    }
}

/// A controllable stand-in for `Task.sleep`: each `sleep()` suspends until `fire()`, and is cancellable.
actor TestSleeper {
    private var waiters: [UUID: CheckedContinuation<Void, Error>] = [:]
    var pendingCount: Int { waiters.count }

    func sleep() async throws {
        let id = UUID()
        try await withTaskCancellationHandler {
            try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, Error>) in
                if Task.isCancelled {
                    continuation.resume(throwing: CancellationError())
                } else {
                    waiters[id] = continuation
                }
            }
        } onCancel: {
            Task { await self.cancel(id) }
        }
    }

    /// Resumes everything currently waiting, after waiting (up to 3 s) for at least one waiter to exist.
    func fire() async {
        let deadline = ContinuousClock.now + .seconds(3)
        while waiters.isEmpty, ContinuousClock.now < deadline { try? await Task.sleep(for: .milliseconds(5)) }
        let current = waiters
        waiters = [:]
        for continuation in current.values { continuation.resume() }
    }

    private func cancel(_ id: UUID) {
        waiters.removeValue(forKey: id)?.resume(throwing: CancellationError())
    }
}

/// Polls until `condition` holds (true) or 3 s pass (false). For asserting that async work finished.
@MainActor
func waitUntil(timeout: Duration = .seconds(3), _ condition: () -> Bool) async -> Bool {
    let deadline = ContinuousClock.now + timeout
    while !condition() {
        if ContinuousClock.now > deadline { return false }
        try? await Task.sleep(for: .milliseconds(5))
    }
    return true
}

/// For asserting that something does NOT happen: gives stray work a moment to (wrongly) run.
func settle() async { try? await Task.sleep(for: .milliseconds(120)) }
