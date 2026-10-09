import Testing
@testable import Repositories

@Suite
struct ListEventTests {
    private func feed(_ lines: [String]) -> [ListEvent] {
        var parser = SSEFrameParser()
        return lines.compactMap { parser.feed($0) }
    }

    private let changed = #"{"type":"item_changed","list_id":"l1","item_id":"i1","version":3}"#

    @Test("A data line ended by a blank line is one event")
    func basic() {
        let events = feed(["event: item_changed", "data: \(changed)", ""])
        #expect(events == [ListEvent(kind: .itemChanged, listID: "l1", itemID: "i1", version: 3)])
    }

    @Test("CRLF endings, keep-alive comments and ignored fields do not matter")
    func crlfAndComments() {
        let events = feed([": keep-alive\r", "", "id: 7\r", "data: \(changed)\r", "\r"])
        #expect(events.count == 1)
        #expect(events.first?.version == 3)
    }

    @Test("A frame split over several data lines is joined with newlines")
    func multiLine() {
        let events = feed([#"data: {"type":"list_changed","#, #"data: "list_id":"l1"}"#, ""])
        #expect(events == [ListEvent(kind: .listChanged, listID: "l1", itemID: nil, version: nil)])
    }

    @Test("Malformed JSON, an unknown type or a missing list id never produce an event")
    func hostile() {
        #expect(feed(["data: {not json", ""]).isEmpty)
        #expect(feed([#"data: {"type":"explode","list_id":"l1"}"#, ""]).isEmpty)
        #expect(feed([#"data: {"type":"list_deleted"}"#, ""]).isEmpty)
        #expect(feed([#"data: ["item_changed"]"#, ""]).isEmpty)
        #expect(feed(["data:", ""]).isEmpty)
    }

    @Test("A blank line with no data, and a frame cut off before its blank line, produce nothing")
    func incomplete() {
        #expect(feed(["", "", ""]).isEmpty)
        #expect(feed(["data: \(changed)"]).isEmpty)
    }

    @Test("The parser resets between frames")
    func resets() {
        let events = feed(["data: \(changed)", "", "data: \(changed)", ""])
        #expect(events.count == 2)
    }

    // MARK: Reducer

    private let list = ShoppingFixtures.list(items: [ShoppingFixtures.item(id: "i1", version: 3)])

    @Test("An event at or below the cached item version is ignored; a newer one refetches")
    func staleRule() {
        func action(_ version: Int) -> ListEventAction {
            ListEventReducer.action(for: ListEvent(kind: .itemChanged, listID: "l1", itemID: "i1", version: version), cached: list)
        }
        #expect(action(2) == .ignore)
        #expect(action(3) == .ignore)
        #expect(action(4) == .refetch)
    }

    @Test("An item_changed for an item the cache has never seen refetches")
    func unknownItem() {
        let event = ListEvent(kind: .itemChanged, listID: "l1", itemID: "new", version: 1)
        #expect(ListEventReducer.action(for: event, cached: list) == .refetch)
        #expect(ListEventReducer.action(for: event, cached: nil) == .refetch)
    }

    @Test("item_deleted removes a cached item whatever its version, and ignores an unknown one")
    func deleted() {
        let known = ListEvent(kind: .itemDeleted, listID: "l1", itemID: "i1", version: 3)
        let unknown = ListEvent(kind: .itemDeleted, listID: "l1", itemID: "zzz", version: 1)
        #expect(ListEventReducer.action(for: known, cached: list) == .removeItem("i1"))
        #expect(ListEventReducer.action(for: unknown, cached: list) == .ignore)
    }

    @Test("list_changed refetches and list_deleted means access is lost")
    func listLevel() {
        #expect(ListEventReducer.action(for: ListEvent(kind: .listChanged, listID: "l1", itemID: nil, version: nil), cached: list) == .refetch)
        #expect(ListEventReducer.action(for: ListEvent(kind: .listDeleted, listID: "l1", itemID: nil, version: nil), cached: list) == .accessLost)
    }
}
