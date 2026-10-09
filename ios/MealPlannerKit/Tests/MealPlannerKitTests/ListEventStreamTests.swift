import Foundation
import Testing
@testable import Repositories

@Suite
struct ListEventStreamTests {
    private let changed = #"data: {"type":"item_changed","list_id":"l1","item_id":"i1","version":2}"#

    private func lines(_ values: [String], failing error: Error? = nil) -> ListEventStream.Lines {
        ListEventStream.Lines { continuation in
            for value in values { continuation.yield(value) }
            continuation.finish(throwing: error)
        }
    }

    private func make(
        token: Locked<String?> = Locked("t1"), refreshed: String? = nil, networkSwitch: NetworkSwitch? = nil,
        open: @escaping ListEventStream.Opener
    ) -> ListEventStream {
        ListEventStream(
            baseURL: URL(string: "http://localhost:8080/v1")!, accessToken: { token.value }, refreshToken: { refreshed },
            networkSwitch: networkSwitch, open: open
        )
    }

    private func collect(_ stream: AsyncThrowingStream<ListEvent, Error>) async throws -> [ListEvent] {
        var events: [ListEvent] = []
        for try await event in stream { events.append(event) }
        return events
    }

    @Test("Opens the list's events URL with the bearer and Accept, and yields parsed events until the server closes")
    func yieldsEvents() async throws {
        let requests = Locked<[URLRequest]>([])
        let stream = make { request in
            requests.mutate { $0.append(request) }
            return (200, lines([": keep-alive", changed, "", "event: list_changed", #"data: {"type":"list_changed","list_id":"l1"}"#, ""]))
        }
        let events = try await collect(stream.events(listID: "l1"))
        #expect(events.map(\.kind) == [.itemChanged, .listChanged])
        let request = try #require(requests.value.first)
        #expect(request.url?.absoluteString == "http://localhost:8080/v1/shopping-lists/l1/events")
        #expect(request.value(forHTTPHeaderField: "Authorization") == "Bearer t1")
        #expect(request.value(forHTTPHeaderField: "Accept") == "text/event-stream")
    }

    @Test("Review focus 5: a frame cut off before its blank line is not delivered, and junk lines do nothing")
    func truncated() async throws {
        let stream = make { _ in (200, lines(["garbage", changed, "", changed])) }
        #expect(try await collect(stream.events(listID: "l1")).count == 1)
    }

    @Test("A 404 means access is lost")
    func notFound() async throws {
        let stream = make { _ in (404, lines([])) }
        await #expect(throws: ListEventStream.StreamError.accessLost) { try await collect(stream.events(listID: "l1")) }
    }

    @Test("A 401 refreshes the token once and retries; a second 401 is an error")
    func unauthorized() async throws {
        let seen = Locked<[String]>([])
        let retried = make(refreshed: "t2") { request in
            seen.mutate { $0.append(request.value(forHTTPHeaderField: "Authorization") ?? "") }
            return seen.value.count == 1 ? (401, lines([])) : (200, lines([changed, ""]))
        }
        #expect(try await collect(retried.events(listID: "l1")).count == 1)
        #expect(seen.value == ["Bearer t1", "Bearer t2"])

        let stuck = make(refreshed: "t2") { _ in (401, lines([])) }
        await #expect(throws: ListEventStream.StreamError.unauthorized) { try await collect(stuck.events(listID: "l1")) }
    }

    @Test("Other statuses are 'unavailable', so the caller retries later")
    func unavailable() async throws {
        let stream = make { _ in (503, lines([])) }
        await #expect(throws: ListEventStream.StreamError.unavailable(503)) { try await collect(stream.events(listID: "l1")) }
    }

    @Test("A dropped connection surfaces as the error that dropped it")
    func dropped() async throws {
        let stream = make { _ in (200, lines([changed, ""], failing: URLError(.networkConnectionLost))) }
        await #expect(throws: URLError.self) { try await collect(stream.events(listID: "l1")) }
    }

    @Test("While the UI-test offline switch is on, nothing is opened")
    func offlineSwitch() async throws {
        let opened = Locked(0)
        let networkSwitch = NetworkSwitch()
        networkSwitch.setOffline(true)
        let stream = make(networkSwitch: networkSwitch) { _ in
            opened.mutate { $0 += 1 }
            return (200, lines([]))
        }
        await #expect(throws: URLError.self) { try await collect(stream.events(listID: "l1")) }
        #expect(opened.value == 0)
    }
}
