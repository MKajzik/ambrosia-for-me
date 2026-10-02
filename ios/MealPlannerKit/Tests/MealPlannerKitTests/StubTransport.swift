import Foundation
import HTTPTypes
import OpenAPIRuntime

/// A `ClientTransport` that counts calls and returns a canned response, so tests can assert
/// on how many real HTTP calls a piece of logic made without touching the network.
actor StubTransport: ClientTransport {
    private(set) var callCount = 0
    private let makeResponse: @Sendable () async throws -> (status: Int, jsonBody: String)

    init(makeResponse: @escaping @Sendable () async throws -> (status: Int, jsonBody: String)) {
        self.makeResponse = makeResponse
    }

    func send(
        _ request: HTTPRequest,
        body: HTTPBody?,
        baseURL: URL,
        operationID: String
    ) async throws -> (HTTPResponse, HTTPBody?) {
        callCount += 1
        let (status, jsonBody) = try await makeResponse()
        // Matches the real API: every error response is RFC 9457 `application/problem+json`
        // (root `CLAUDE.md`), never plain `application/json`.
        let contentType = status >= 400 ? "application/problem+json" : "application/json"
        let response = HTTPResponse(
            status: .init(code: status),
            headerFields: [.contentType: contentType]
        )
        return (response, HTTPBody(jsonBody))
    }
}
