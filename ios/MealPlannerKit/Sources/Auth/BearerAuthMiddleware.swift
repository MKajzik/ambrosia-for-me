import Foundation
import HTTPTypes
import OpenAPIRuntime

/// Attaches the current access token to every request, and on a `401`, refreshes once
/// (single-flighted by `TokenRefresher`) and retries the request exactly once with the new
/// token. Never retries a request whose body has already been consumed by a failed attempt.
public struct BearerAuthMiddleware: ClientMiddleware {
    private let refresher: TokenRefresher

    public init(refresher: TokenRefresher) {
        self.refresher = refresher
    }

    public func intercept(
        _ request: HTTPRequest,
        body: HTTPBody?,
        baseURL: URL,
        operationID: String,
        next: (HTTPRequest, HTTPBody?, URL) async throws -> (HTTPResponse, HTTPBody?)
    ) async throws -> (HTTPResponse, HTTPBody?) {
        var request = request
        if let token = await refresher.currentAccessToken() {
            request.headerFields[.authorization] = "Bearer \(token)"
        }
        let (response, responseBody) = try await next(request, body, baseURL)
        guard response.status.code == 401 else {
            return (response, responseBody)
        }
        if let body, body.iterationBehavior != .multiple {
            // The body was a single-use stream already consumed by the first attempt; a retry
            // would send an empty body, so give up rather than send a broken request.
            return (response, responseBody)
        }
        guard let newToken = try? await refresher.refreshAccessToken() else {
            return (response, responseBody)
        }
        var retried = request
        retried.headerFields[.authorization] = "Bearer \(newToken)"
        return try await next(retried, body, baseURL)
    }
}
