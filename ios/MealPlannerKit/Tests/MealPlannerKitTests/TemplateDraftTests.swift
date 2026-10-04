import API
import Testing
@testable import Features

@Suite
struct TemplateDraftTests {
    private func template(slots: [Components.Schemas.TemplateSlot] = []) -> Components.Schemas.DietTemplate {
        Fixtures.template(name: "Cut", dayCount: 3, slots: slots)
    }

    private func valid(_ d: TemplateDraft) -> TemplateDraft.Valid? {
        if case .valid(let v) = d.validate() { v } else { nil }
    }

    private func errors(_ d: TemplateDraft) -> TemplateDraft.Errors? {
        if case .invalid(let e) = d.validate() { e } else { nil }
    }

    @Test("A draft built from a template validates to what the server holds")
    func fromTemplate() {
        let t = template(slots: [Fixtures.templateSlot(dayIndex: 1, slot: .lunch, portion: 1.5)])
        let d = TemplateDraft(template: t)
        #expect(d.name == "Cut")
        #expect(d.dayCount == 3)
        #expect(d.rows.map(\.portion) == ["1.5"])
        #expect(valid(d) == TemplateDraft.saved(from: t))
    }

    @Test("Slots are canonical: by day, then breakfast, lunch, dinner, snack, then insertion order, whatever the server sent")
    func canonicalOrder() {
        let shuffled = template(slots: [
            Fixtures.templateSlot(id: "s2", dayIndex: 1, slot: .snack, mealID: "b"),
            Fixtures.templateSlot(id: "d", dayIndex: 0, slot: .dinner),
            Fixtures.templateSlot(id: "s1", dayIndex: 1, slot: .snack, mealID: "a"),
            Fixtures.templateSlot(id: "bk", dayIndex: 0, slot: .breakfast),
            Fixtures.templateSlot(id: "l1", dayIndex: 1, slot: .lunch),
        ])
        let order = TemplateDraft.saved(from: shuffled).slots.map { "\($0.dayIndex)\($0.slot.rawValue.prefix(1))\($0.mealId)" }
        #expect(order == ["0bm1", "0dm1", "1lm1", "1sb", "1sa"])
        #expect(valid(TemplateDraft(template: shuffled)) == TemplateDraft.saved(from: shuffled))
    }

    @Test("Name is trimmed and limited to 1–200 characters")
    func nameLimits() {
        var d = TemplateDraft(template: template())
        d.name = "  "
        #expect(errors(d)?.name == "Give the template a name.")
        d.name = String(repeating: "a", count: 201)
        #expect(errors(d)?.name == "Use at most 200 characters.")
        d.name = String(repeating: "a", count: 200)
        #expect(valid(d) != nil)
        d.name = "  Bulk  "
        #expect(valid(d)?.name == "Bulk")
    }

    @Test(arguments: ["1", "0.01", "100", "1,5", "2."])
    func validPortions(text: String) {
        var d = TemplateDraft(template: template())
        d.setMeal(dayIndex: 0, slot: .breakfast, mealID: "m1", mealName: "Oats")
        d.rows[0].portion = text
        #expect(valid(d) != nil)
    }

    @Test("Out-of-range portions get the range message", arguments: ["0", "100.5", "101"])
    func portionRange(text: String) {
        var d = TemplateDraft(template: template())
        d.setMeal(dayIndex: 0, slot: .breakfast, mealID: "m1", mealName: "Oats")
        d.rows[0].portion = text
        #expect(errors(d)?.rows[d.rows[0].id] == "The portion must be more than 0 and at most 100.")
    }

    @Test("Blank and non-numeric portions ask for a portion", arguments: ["", "  ", "abc", "-1", "1e2"])
    func portionInput(text: String) {
        var d = TemplateDraft(template: template())
        d.setMeal(dayIndex: 0, slot: .breakfast, mealID: "m1", mealName: "Oats")
        d.rows[0].portion = text
        #expect(errors(d)?.rows[d.rows[0].id] == "Enter a portion.")
    }

    @Test("A slot outside the template's days is invalid")
    func dayOutsideTemplate() {
        var d = TemplateDraft(template: template())
        d.setMeal(dayIndex: 3, slot: .lunch, mealID: "m1", mealName: "Oats")
        #expect(errors(d)?.rows[d.rows[0].id] == "That day is outside the template.")
    }

    @Test("Choosing a meal for an occupied non-snack slot replaces it and keeps the portion; a snack is added")
    func setMeal() {
        var d = TemplateDraft(template: template(slots: [Fixtures.templateSlot(id: "x", dayIndex: 0, slot: .breakfast, mealID: "m1", mealName: "Oats", portion: 2)]))
        d.setMeal(dayIndex: 0, slot: .breakfast, mealID: "m2", mealName: "Rice")
        #expect(d.rows.count == 1)
        #expect(d.rows[0].id == "x")
        #expect(d.rows[0].mealName == "Rice")
        #expect(d.rows[0].portion == "2")
        d.setMeal(dayIndex: 0, slot: .snack, mealID: "m1", mealName: "Oats")
        d.setMeal(dayIndex: 0, slot: .snack, mealID: "m2", mealName: "Rice")
        #expect(d.rows(day: 0, slot: .snack).count == 2)
        #expect(d.rows(day: 0, slot: .snack).allSatisfy { $0.portion == "1" })
        #expect(Set(d.rows.map(\.id)).count == 3)
    }

    @Test("removeRow removes just that slot")
    func removeRow() {
        var d = TemplateDraft(template: template(slots: [Fixtures.templateSlot(id: "a"), Fixtures.templateSlot(id: "b", dayIndex: 1)]))
        d.removeRow(id: "a")
        #expect(d.rows.map(\.id) == ["b"])
    }

    @Test("No change gives no writes")
    func noChange() throws {
        let saved = try #require(valid(TemplateDraft(template: template(slots: [Fixtures.templateSlot()]))))
        #expect(TemplateDraft.changes(from: saved, to: saved).isEmpty)
    }

    @Test("A renamed or shared template patches only those fields and sends no slots")
    func patchOnly() throws {
        let saved = try #require(valid(TemplateDraft(template: template(slots: [Fixtures.templateSlot()]))))
        var next = saved
        next.name = "Bulk"
        let renamed = TemplateDraft.changes(from: saved, to: next)
        #expect(renamed.patch == Components.Schemas.UpdateDietTemplateRequest(name: "Bulk"))
        #expect(renamed.slots == nil)
        next = saved
        next.shared = true
        #expect(TemplateDraft.changes(from: saved, to: next).patch == .init(sharedWithPartner: true))
    }

    @Test("A changed slot sends the whole slot list")
    func slotsChange() throws {
        let saved = try #require(valid(TemplateDraft(template: template(slots: [Fixtures.templateSlot()]))))
        var d = TemplateDraft(template: template(slots: [Fixtures.templateSlot()]))
        d.rows[0].portion = "2"
        let next = try #require(valid(d))
        let changes = TemplateDraft.changes(from: saved, to: next)
        #expect(changes.patch == nil)
        #expect(changes.slots == next.slots)
    }

    @Test("Removing the last slot sends an empty list rather than skipping the write")
    func removingLastSlot() throws {
        let saved = try #require(valid(TemplateDraft(template: template(slots: [Fixtures.templateSlot(id: "a")]))))
        var d = TemplateDraft(template: template(slots: [Fixtures.templateSlot(id: "a")]))
        d.removeRow(id: "a")
        let next = try #require(valid(d))
        #expect(TemplateDraft.changes(from: saved, to: next).slots == [])
    }

    @Test("Create-form helper shares the editor's name messages")
    func nameError() {
        #expect(TemplateDraft.nameError("") == "Give the template a name.")
        #expect(TemplateDraft.nameError("Soup") == nil)
    }
}
