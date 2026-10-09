import Foundation
import HTTPTypes
import OpenAPIRuntime

/// A switch the UI tests flip to cut the app off from the API (the simulator cannot be told to go offline mid-test).
/// Built only when the app is launched with `-uiTesting`; in a normal launch nothing creates one.
public final class NetworkSwitch: @unchecked Sendable {
    private let lock = NSLock()
    private var offline = false

    public init() {}

    public var isOffline: Bool {
        lock.lock(); defer { lock.unlock() }
        return offline
    }

    public func setOffline(_ value: Bool) {
        lock.lock(); offline = value; lock.unlock()
    }
}

/// Fails every request with `URLError(.notConnectedToInternet)` while the switch is offline, before it reaches the network.
public struct OfflineMiddleware: ClientMiddleware {
    private let networkSwitch: NetworkSwitch

    public init(_ networkSwitch: NetworkSwitch) { self.networkSwitch = networkSwitch }

    public func intercept(
        _ request: HTTPRequest,
        body: HTTPBody?,
        baseURL: URL,
        operationID: String,
        next: (HTTPRequest, HTTPBody?, URL) async throws -> (HTTPResponse, HTTPBody?)
    ) async throws -> (HTTPResponse, HTTPBody?) {
        if networkSwitch.isOffline { throw URLError(.notConnectedToInternet) }
        return try await next(request, body, baseURL)
    }
}
