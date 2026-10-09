import API
import Testing
@testable import Features

@Suite
struct ItemGroupingTests {
    @Test("Groups follow the API's category order and skip empty aisles; unchecked come first, then by position")
    func groups() {
        let items = [
            ShoppingFixtures.item(id: "a", name: "Soda", category: .beverages, position: 0),
            ShoppingFixtures.item(id: "b", name: "Apple", category: .produce, checked: true, position: 1),
            ShoppingFixtures.item(id: "c", name: "Leek", category: .produce, position: 3),
            ShoppingFixtures.item(id: "d", name: "Kale", category: .produce, position: 2),
        ]
        let groups = ItemGrouping.groups(items)
        #expect(groups.map(\.category) == [.produce, .beverages])
        #expect(groups[0].items.map(\.id) == ["d", "c", "b"])
    }

    @Test("Progress counts checked of total")
    func progress() {
        let items = [ShoppingFixtures.item(id: "a", checked: true), ShoppingFixtures.item(id: "b")]
        #expect(ItemGrouping.progress(items) == (done: 1, total: 2))
    }

    @Test("Quantity text: nothing for null, never 0; pieces pluralise; up to two decimals")
    func quantity() {
        #expect(ItemGrouping.quantityText(ShoppingFixtures.item()) == "")
        #expect(ItemGrouping.quantityText(ShoppingFixtures.item(quantity: 2, unit: .g)) == "2 g")
        #expect(ItemGrouping.quantityText(ShoppingFixtures.item(quantity: 1.5, unit: .ml)) == "1.5 ml")
        #expect(ItemGrouping.quantityText(ShoppingFixtures.item(quantity: 0.333, unit: .g)) == "0.33 g")
        #expect(ItemGrouping.quantityText(ShoppingFixtures.item(quantity: 1, unit: .piece)) == "1 piece")
        #expect(ItemGrouping.quantityText(ShoppingFixtures.item(quantity: 3, unit: .piece)) == "3 pieces")
        #expect(ItemGrouping.quantityText(ShoppingFixtures.item(quantity: 4)) == "4")
        #expect(ItemGrouping.quantityText(ShoppingFixtures.item(quantity: 1000, unit: .g)) == "1,000 g")
    }
}
