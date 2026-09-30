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

/// Builds a `Client` with no middleware. Used only for the one call that must never recurse
/// into the auth middleware: exchanging a refresh token for a new access token (Task 3).
public func makeAuthlessClient(baseURL: URL = APIEnvironment.baseURL) -> Client {
    Client(serverURL: baseURL, transport: URLSessionTransport())
}

/// Builds a `Client` with the given middlewares (Task 3 supplies the bearer-auth middleware).
public func makeClient(baseURL: URL = APIEnvironment.baseURL, middlewares: [any ClientMiddleware]) -> Client {
    Client(serverURL: baseURL, transport: URLSessionTransport(), middlewares: middlewares)
}
