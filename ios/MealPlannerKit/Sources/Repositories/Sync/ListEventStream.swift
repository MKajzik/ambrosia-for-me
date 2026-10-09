import Foundation

/// The client for `GET /shopping-lists/{id}/events`. iOS has no `EventSource`, so this reads the response bytes itself,
/// with the bearer in `Authorization` like every other route. It yields events until the server closes the stream; the
/// caller decides what to do next (refetch, reconnect with backoff).
public struct ListEventStream: Sendable {
    public typealias Lines = AsyncThrowingStream<String, Error>
    /// Opens the request and returns its status and its lines (empty lines included: they end a frame).
    public typealias Opener = @Sendable (URLRequest) async throws -> (status: Int, lines: Lines)

    public enum StreamError: Error, Equatable, Sendable {
        /// `404`: the list is gone, or the person can no longer see it.
        case accessLost
        case unauthorized
        case unavailable(Int)
    }

    private let baseURL: URL
    private let accessToken: @Sendable () async -> String?
    private let refreshToken: @Sendable () async -> String?
    private let networkSwitch: NetworkSwitch?
    private let open: Opener

    public init(
        baseURL: URL,
        accessToken: @escaping @Sendable () async -> String?,
        refreshToken: @escaping @Sendable () async -> String?,
        networkSwitch: NetworkSwitch? = nil,
        open: @escaping Opener = ListEventStream.urlSessionOpener
    ) {
        self.baseURL = baseURL
        self.accessToken = accessToken
        self.refreshToken = refreshToken
        self.networkSwitch = networkSwitch
        self.open = open
    }

    public func events(listID: String) -> AsyncThrowingStream<ListEvent, Error> {
        AsyncThrowingStream { continuation in
            let task = Task {
                do {
                    if networkSwitch?.isOffline == true { throw URLError(.notConnectedToInternet) }
                    var result = try await open(request(listID: listID, token: await accessToken()))
                    if result.status == 401, let fresh = await refreshToken() {
                        result = try await open(request(listID: listID, token: fresh))
                    }
                    switch result.status {
                    case 200: break
                    case 401: throw StreamError.unauthorized
                    case 404: throw StreamError.accessLost
                    default: throw StreamError.unavailable(result.status)
                    }
                    var parser = SSEFrameParser()
                    for try await line in result.lines {
                        if let event = parser.feed(line) { continuation.yield(event) }
                    }
                    continuation.finish()
                } catch {
                    continuation.finish(throwing: error)
                }
            }
            continuation.onTermination = { _ in task.cancel() }
        }
    }

    private func request(listID: String, token: String?) -> URLRequest {
        var request = URLRequest(url: baseURL.appending(path: "shopping-lists/\(listID)/events"))
        request.setValue("text/event-stream", forHTTPHeaderField: "Accept")
        if let token { request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization") }
        return request
    }

    /// The real transport. `URLSession.AsyncBytes.lines` drops empty lines, and an empty line is what ends an SSE frame,
    /// so the bytes are split on `\n` by hand.
    public static let urlSessionOpener: Opener = { request in
        let (bytes, response) = try await URLSession.shared.bytes(for: request)
        let status = (response as? HTTPURLResponse)?.statusCode ?? 0
        let lines = Lines { continuation in
            let task = Task {
                do {
                    var line: [UInt8] = []
                    for try await byte in bytes {
                        if byte == 0x0A {
                            continuation.yield(String(decoding: line, as: UTF8.self))
                            line.removeAll(keepingCapacity: true)
                        } else {
                            line.append(byte)
                        }
                    }
                    if !line.isEmpty { continuation.yield(String(decoding: line, as: UTF8.self)) }
                    continuation.finish()
                } catch {
                    continuation.finish(throwing: error)
                }
            }
            continuation.onTermination = { _ in task.cancel() }
        }
        return (status, lines)
    }
}
