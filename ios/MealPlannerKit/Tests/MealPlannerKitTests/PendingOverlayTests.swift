import API
import Foundation
import Persistence
import Testing
@testable import Repositories

@Suite
struct PendingOverlayTests {
    private func intent(_ kind: ShoppingIntent.Kind, _ item: String, seq: Int, list: String = "l1", payload: ShoppingIntent.AddPayload? = nil) -> ShoppingIntent {
        ShoppingIntent(sequence: seq, kind: kind, listID: list, itemID: item, payload: payload)
    }

    private let base = ShoppingFixtures.list(items: [
        ShoppingFixtures.item(id: "i1", name: "Milk", position: 0),
        ShoppingFixtures.item(id: "i2", name: "Eggs", checked: true, checkedBy: "u9", position: 1),
    ])

    @Test("A check flips the item and records who; version is left to the server")
    func check() {
        let shown = PendingOverlay.apply(base, intents: [intent(.check, "i1", seq: 1)], userID: "u1")
        let milk = shown.items[0]
        #expect(milk.checked)
        #expect(milk.checkedBy == "u1")
        #expect(milk.version == 1)
    }

    @Test("An uncheck clears the checker")
    func uncheck() {
        let shown = PendingOverlay.apply(base, intents: [intent(.uncheck, "i2", seq: 1)], userID: "u1")
        #expect(shown.items[1].checked == false)
        #expect(shown.items[1].checkedBy == nil)
    }

    @Test("A remove hides the item")
    func remove() {
        let shown = PendingOverlay.apply(base, intents: [intent(.remove, "i1", seq: 1)], userID: nil)
        #expect(shown.items.map(\.id) == ["i2"])
    }

    @Test("An add appears as a temp item at the end, version 1, category defaulting to other")
    func add() throws {
        let temp = ShoppingIntent.newTempID()
        let add = intent(.add, temp, seq: 1, payload: .init(name: "Bread", quantity: 2, unit: .piece))
        let shown = PendingOverlay.apply(base, intents: [add], userID: nil)
        let bread = try #require(shown.items.last)
        #expect(bread.id == temp)
        #expect(bread.name == "Bread")
        #expect(bread.category == .other)
        #expect(bread.unit == .piece)
        #expect(bread.version == 1)
        #expect(bread.position == 2)
        #expect(bread.origin == .manual)
    }

    @Test("A check on a pending add applies to the temp item")
    func checkOnTemp() {
        let temp = ShoppingIntent.newTempID()
        let shown = PendingOverlay.apply(
            base,
            intents: [intent(.add, temp, seq: 1, payload: .init(name: "Bread")), intent(.check, temp, seq: 2)],
            userID: "u1"
        )
        #expect(shown.items.last?.checked == true)
    }

    @Test("Intents apply in sequence order whatever order they are given in")
    func ordering() {
        let shown = PendingOverlay.apply(
            base, intents: [intent(.uncheck, "i1", seq: 3), intent(.check, "i1", seq: 2)], userID: "u1"
        )
        #expect(shown.items[0].checked == false)
    }

    @Test("Intents for another list are ignored, and the snapshot is never mutated")
    func otherList() {
        let shown = PendingOverlay.apply(base, intents: [intent(.remove, "i1", seq: 1, list: "other")], userID: nil)
        #expect(shown == base)
    }

    @Test("An add whose server item already exists is not shown twice")
    func addNotDuplicated() {
        let temp = ShoppingIntent.newTempID()
        let list = ShoppingFixtures.list(items: [ShoppingFixtures.item(id: temp)])
        let shown = PendingOverlay.apply(list, intents: [intent(.add, temp, seq: 1, payload: .init(name: "Milk"))], userID: nil)
        #expect(shown.items.count == 1)
    }
}
