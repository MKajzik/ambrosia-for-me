import Foundation
import OpenAPIRuntime
import OpenAPIURLSession

public enum APIEnvironment {
    /// Matches `openapi.yaml`'s `servers[0]`. A `let`, not a `var`: Swift 6 strict concurrency
    /// flags a mutable global as unsafe shared state, and nothing in this app needs to change
    /// the base URL after launch — `RootView.init(baseURL:)` already takes an override for
    /// tests or a differently-hosted device.
    public static let baseURL = URL(string: "http://localhost:8080/v1")!
}

/// The generated client's default `Configuration` decodes `date-time` strings with
/// `.iso8601`, which cannot parse fractional seconds. The Go backend's `time.Time` JSON
/// marshaling emits fractional seconds (e.g. `2026-10-01T08:21:01.92863+02:00`), so every
/// response carrying a timestamp (every `User`, among others) would otherwise fail to decode.
private let apiConfiguration = Configuration(dateTranscoder: .iso8601WithFractionalSeconds)

/// Builds a `Client` with no middleware. Used only for the one call that must never recurse
/// into the auth middleware: exchanging a refresh token for a new access token (Task 3).
public func makeAuthlessClient(baseURL: URL = APIEnvironment.baseURL) -> Client {
    Client(serverURL: baseURL, configuration: apiConfiguration, transport: URLSessionTransport())
}

/// Test-only seam: build a client against an arbitrary transport instead of `URLSessionTransport`.
public func makeAuthlessClient(baseURL: URL = APIEnvironment.baseURL, transport: any ClientTransport) -> Client {
    Client(serverURL: baseURL, configuration: apiConfiguration, transport: transport)
}

/// Builds a `Client` with the given middlewares (Task 3 supplies the bearer-auth middleware).
public func makeClient(baseURL: URL = APIEnvironment.baseURL, middlewares: [any ClientMiddleware]) -> Client {
    Client(serverURL: baseURL, configuration: apiConfiguration, transport: URLSessionTransport(), middlewares: middlewares)
}

/// Test seam: a client with middlewares over an arbitrary transport (the production `makeClient` always uses `URLSessionTransport`).
public func makeClient(
    baseURL: URL = APIEnvironment.baseURL, transport: any ClientTransport, middlewares: [any ClientMiddleware]
) -> Client {
    Client(serverURL: baseURL, configuration: apiConfiguration, transport: transport, middlewares: middlewares)
}
