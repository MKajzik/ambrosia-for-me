import API
import Testing
@testable import Features

@Suite
struct MealDraftTests {
    private func draft(name: String = "Pasta", notes: String = "", servings: String = "2", rows: [MealDraft.Row] = []) -> MealDraft {
        MealDraft(name: name, notes: notes, servings: servings, shared: false, rows: rows)
    }

    private func row(_ quantity: String, id: String = "r1", unit: Components.Schemas.Unit = .g) -> MealDraft.Row {
        .init(id: id, ingredientID: "ing-1", name: "Rice", category: .grainsBread, quantity: quantity, unit: unit)
    }

    private func valid(_ d: MealDraft) -> MealDraft.Valid? {
        if case .valid(let v) = d.validate() { v } else { nil }
    }

    private func errors(_ d: MealDraft) -> MealDraft.Errors? {
        if case .invalid(let e) = d.validate() { e } else { nil }
    }

    @Test("A draft built from a meal round-trips and validates to what the server holds")
    func fromMeal() {
        let meal = Fixtures.meal(notes: "Quick", servings: 2, shared: true, lines: [Fixtures.line(quantity: 150)])
        let d = MealDraft(meal: meal)
        #expect(d.name == "Pasta")
        #expect(d.servings == "2")
        #expect(d.rows.map(\.quantity) == ["150"])
        #expect(valid(d) == MealDraft.saved(from: meal))
    }

    @Test("Name is trimmed and limited to 1–200 characters")
    func nameLimits() {
        #expect(errors(draft(name: ""))?.name == "Give the meal a name.")
        #expect(errors(draft(name: "   "))?.name == "Give the meal a name.")
        #expect(errors(draft(name: String(repeating: "a", count: 201)))?.name == "Use at most 200 characters.")
        #expect(valid(draft(name: String(repeating: "a", count: 200))) != nil)
        #expect(valid(draft(name: "  Soup  "))?.name == "Soup")
    }

    @Test("Notes are limited to 2000 characters, trimmed, and blank means none")
    func notes() {
        #expect(errors(draft(notes: String(repeating: "n", count: 2001)))?.notes == "Use at most 2000 characters.")
        #expect(valid(draft(notes: String(repeating: "n", count: 2000)))?.notes?.count == 2000)
        #expect(valid(draft(notes: "   "))?.notes == nil)
        #expect(valid(draft(notes: " hi "))?.notes == "hi")
    }

    @Test(arguments: [("0", false), ("0.001", true), ("1000", true), ("1001", false), ("abc", false), ("", false), ("-1", false), ("1,5", true), ("2.", true)])
    func servingsLimits(text: String, ok: Bool) {
        #expect((valid(draft(servings: text)) != nil) == ok)
        if !ok { #expect(errors(draft(servings: text))?.servings == "Servings must be more than 0 and at most 1000.") }
    }

    @Test("A quantity is more than 0 and at most 100000; blank and junk ask for an amount")
    func quantityLimits() {
        #expect(valid(draft(rows: [row("100000")])) != nil)
        #expect(errors(draft(rows: [row("100001")]))?.rows["r1"] == "The amount must be more than 0 and at most 100000.")
        #expect(errors(draft(rows: [row("0")]))?.rows["r1"] == "The amount must be more than 0 and at most 100000.")
        #expect(errors(draft(rows: [row("")]))?.rows["r1"] == "Enter an amount.")
        #expect(errors(draft(rows: [row("12 g")]))?.rows["r1"] == "Enter an amount.")
        #expect(valid(draft(rows: [row("2,5")]))?.items.first?.quantity == 2.5)
    }

    @Test("At most 200 ingredients; the 201st cannot be added")
    func ingredientCap() {
        let many = (0..<201).map { row("100", id: "r\($0)") }
        #expect(errors(draft(rows: many))?.ingredients == "A meal can have at most 200 ingredients.")
        var d = draft(rows: Array(many.prefix(200)))
        #expect(!d.canAddRow)
        d.addRow(for: Fixtures.ingredient())
        #expect(d.rows.count == 200)
        var small = draft()
        small.addRow(for: Fixtures.ingredient(name: "Oats"))
        #expect(small.rows.count == 1)
        #expect(small.rows[0].quantity == "100")
        #expect(small.rows[0].unit == .g)
        #expect(small.rows[0].name == "Oats")
    }

    @Test("Two new rows never share a key")
    func rowKeys() {
        var d = draft()
        d.addRow(for: Fixtures.ingredient())
        d.addRow(for: Fixtures.ingredient())
        #expect(Set(d.rows.map(\.id)).count == 2)
    }

    @Test("No change gives no writes")
    func noChange() throws {
        let saved = try #require(valid(MealDraft(meal: Fixtures.meal(lines: [Fixtures.line()]))))
        #expect(MealDraft.changes(from: saved, to: saved).isEmpty)
    }

    @Test("A changed field produces a patch of only that field")
    func patchOnlyChangedFields() throws {
        let saved = try #require(valid(MealDraft(meal: Fixtures.meal())))
        var next = saved
        next.name = "Lasagne"
        let changes = MealDraft.changes(from: saved, to: next)
        #expect(changes.patch == Components.Schemas.UpdateMealRequest(name: "Lasagne"))
        #expect(changes.items == nil)
        next = saved
        next.shared = true
        #expect(MealDraft.changes(from: saved, to: next).patch == .init(sharedWithPartner: true))
        next = saved
        next.servings = 4
        #expect(MealDraft.changes(from: saved, to: next).patch == .init(servings: 4))
    }

    @Test("Changing a row sends the whole ingredient list")
    func itemsChange() throws {
        let saved = try #require(valid(MealDraft(meal: Fixtures.meal(lines: [Fixtures.line(quantity: 100)]))))
        var next = saved
        next.items[0].quantity = 150
        let changes = MealDraft.changes(from: saved, to: next)
        #expect(changes.patch == nil)
        #expect(changes.items == next.items)
    }

    @Test("Removing the last ingredient sends an empty list rather than skipping the write")
    func removingLastIngredient() throws {
        let saved = try #require(valid(MealDraft(meal: Fixtures.meal(lines: [Fixtures.line()]))))
        var next = saved
        next.items = []
        #expect(MealDraft.changes(from: saved, to: next).items == [])
    }

    @Test("Clearing notes sends an empty string, and the server's empty string is not a change")
    func clearingNotes() throws {
        let saved = try #require(valid(MealDraft(meal: Fixtures.meal(notes: "Quick"))))
        let cleared = try #require(valid(draft(notes: "")))
        var next = saved
        next.notes = cleared.notes
        #expect(MealDraft.changes(from: saved, to: next).patch == Components.Schemas.UpdateMealRequest(notes: ""))
        // After the PATCH the server answers notes: "" — it must read back as "no notes", not as a pending edit.
        let afterClear = MealDraft.saved(from: Fixtures.meal(notes: ""))
        #expect(afterClear.notes == nil)
        #expect(MealDraft.changes(from: afterClear, to: next).isEmpty)
    }

    @Test("A new meal with no notes does not diff against a blank notes field")
    func nilNotesEqualsBlank() throws {
        let saved = MealDraft.saved(from: Fixtures.meal(notes: nil))
        let next = try #require(valid(MealDraft(meal: Fixtures.meal(notes: nil))))
        #expect(MealDraft.changes(from: saved, to: next).isEmpty)
    }

    @Test("Create-form helpers share the editor's messages")
    func createHelpers() {
        #expect(MealDraft.nameError("") == "Give the meal a name.")
        #expect(MealDraft.nameError("Soup") == nil)
        #expect(MealDraft.servingsValue("1,5") == 1.5)
        #expect(MealDraft.servingsValue("0") == nil)
        #expect(MealDraft.servingsMessage == "Servings must be more than 0 and at most 1000.")
    }
}
