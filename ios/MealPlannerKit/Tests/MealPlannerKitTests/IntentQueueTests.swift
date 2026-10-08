import Testing
@testable import Persistence

@Suite
struct IntentQueueTests {
    private func intent(_ kind: ShoppingIntent.Kind, _ item: String, seq: Int, list: String = "l1") -> ShoppingIntent {
        ShoppingIntent(sequence: seq, kind: kind, listID: list, itemID: item, payload: kind == .add ? .init(name: "Milk") : nil)
    }

    private func kinds(_ queue: [ShoppingIntent]) -> [String] { queue.map { "\($0.kind.rawValue):\($0.itemID)" } }

    @Test("A check replaces an earlier pending check of the same item: last write wins")
    func checkReplacesCheck() {
        var queue: [ShoppingIntent] = []
        queue = IntentQueue.enqueue(intent(.check, "i1", seq: 1), into: queue)
        queue = IntentQueue.enqueue(intent(.check, "i2", seq: 2), into: queue)
        queue = IntentQueue.enqueue(intent(.uncheck, "i1", seq: 3), into: queue)
        // Review focus 1: two quick taps on i1 leave one row holding the last state.
        #expect(kinds(queue) == ["check:i2", "uncheck:i1"])
    }

    @Test("Checking, unchecking and checking again leaves one check")
    func tripleToggle() {
        var queue: [ShoppingIntent] = []
        for (n, kind) in [ShoppingIntent.Kind.check, .uncheck, .check].enumerated() {
            queue = IntentQueue.enqueue(intent(kind, "i1", seq: n + 1), into: queue)
        }
        #expect(kinds(queue) == ["check:i1"])
    }

    @Test("An add followed by a remove of the same temp item cancels both, and any check in between")
    func addThenRemoveCancels() {
        let temp = ShoppingIntent.newTempID()
        var queue: [ShoppingIntent] = []
        queue = IntentQueue.enqueue(intent(.add, temp, seq: 1), into: queue)
        queue = IntentQueue.enqueue(intent(.check, temp, seq: 2), into: queue)
        queue = IntentQueue.enqueue(intent(.check, "i9", seq: 3), into: queue)
        queue = IntentQueue.enqueue(intent(.remove, temp, seq: 4), into: queue)
        #expect(kinds(queue) == ["check:i9"])
    }

    @Test("A check on a pending add keeps its own row, after the add")
    func checkOnPendingAdd() {
        let temp = ShoppingIntent.newTempID()
        var queue: [ShoppingIntent] = []
        queue = IntentQueue.enqueue(intent(.add, temp, seq: 1), into: queue)
        queue = IntentQueue.enqueue(intent(.check, temp, seq: 2), into: queue)
        #expect(queue.map(\.kind) == [.add, .check])
        #expect(queue.allSatisfy { $0.isTemp })
    }

    @Test("Removing a synced item drops its pending checks and queues one remove")
    func removeSynced() {
        var queue: [ShoppingIntent] = []
        queue = IntentQueue.enqueue(intent(.check, "i1", seq: 1), into: queue)
        queue = IntentQueue.enqueue(intent(.remove, "i1", seq: 2), into: queue)
        queue = IntentQueue.enqueue(intent(.remove, "i1", seq: 3), into: queue)
        #expect(kinds(queue) == ["remove:i1"])
        #expect(queue.first?.sequence == 2)
    }

    @Test("A check of an item with a pending remove is ignored")
    func checkAfterRemove() {
        var queue: [ShoppingIntent] = []
        queue = IntentQueue.enqueue(intent(.remove, "i1", seq: 1), into: queue)
        queue = IntentQueue.enqueue(intent(.check, "i1", seq: 2), into: queue)
        #expect(kinds(queue) == ["remove:i1"])
    }

    @Test("Adds keep their order and different lists never interfere")
    func addsAndLists() {
        var queue: [ShoppingIntent] = []
        queue = IntentQueue.enqueue(intent(.add, "temp:a", seq: 1), into: queue)
        queue = IntentQueue.enqueue(intent(.add, "temp:b", seq: 2, list: "l2"), into: queue)
        queue = IntentQueue.enqueue(intent(.add, "temp:c", seq: 3), into: queue)
        #expect(kinds(queue) == ["add:temp:a", "add:temp:b", "add:temp:c"])
    }

    @Test("newTempID is recognised as temp and is unique")
    func tempIDs() {
        let a = ShoppingIntent.newTempID()
        let b = ShoppingIntent.newTempID()
        #expect(a != b)
        #expect(ShoppingIntent(sequence: 1, kind: .check, listID: "l", itemID: a).isTemp)
        #expect(!ShoppingIntent(sequence: 1, kind: .check, listID: "l", itemID: "5f1c").isTemp)
    }
}
