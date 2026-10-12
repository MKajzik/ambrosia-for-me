import Foundation
import HTTPTypes
import OpenAPIRuntime

/// The generated request types are plain optionals and cannot encode an explicit JSON `null`, but `PATCH /me` and
/// `PATCH /ingredients/{id}` use `null` to clear a field (an omitted field means "unchanged"). A repository that wants
/// to clear one sends `NullSentinel.value`, and `NullSentinelMiddleware` turns it into `null` on the way out.
///
/// The value is invalid for every field that can be cleared (they must be above 0, or 0 or more), so if the
/// middleware ever failed to run the server would answer `400` instead of changing anything.
public enum NullSentinel {
    public static let value: Double = -1
}

public struct NullSentinelMiddleware: ClientMiddleware {
    /// The only operations whose top-level numbers are rewritten.
    public static let operations: Set<String> = ["updateMe", "updateIngredient"]

    public init() {}

    public func intercept(
        _ request: HTTPRequest,
        body: HTTPBody?,
        baseURL: URL,
        operationID: String,
        next: (HTTPRequest, HTTPBody?, URL) async throws -> (HTTPResponse, HTTPBody?)
    ) async throws -> (HTTPResponse, HTTPBody?) {
        guard Self.operations.contains(operationID), let body else {
            return try await next(request, body, baseURL)
        }
        let data = try await Data(collecting: body, upTo: 1_048_576)
        guard var object = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any] else {
            // Not a JSON object: send exactly what we were given (the original body was consumed above).
            return try await next(request, HTTPBody(data), baseURL)
        }
        // Top-level fields only. Only a number can equal the sentinel; a boolean is 0 or 1 and never matches.
        for (key, value) in object {
            if let number = value as? NSNumber, number.doubleValue == NullSentinel.value {
                object[key] = NSNull()
            }
        }
        let rewritten = try JSONSerialization.data(withJSONObject: object)
        var request = request
        request.headerFields[.contentLength] = String(rewritten.count)
        return try await next(request, HTTPBody(rewritten), baseURL)
    }
}
