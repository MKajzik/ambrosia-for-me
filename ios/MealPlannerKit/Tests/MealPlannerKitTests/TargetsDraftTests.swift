import Testing
@testable import Features
@testable import Repositories

@Suite
struct TargetsDraftTests {
    private func draft(_ c: String = "", _ p: String = "", _ ca: String = "", _ f: String = "") -> TargetsDraft {
        var draft = TargetsDraft()
        draft.calories = c
        draft.protein = p
        draft.carbs = ca
        draft.fat = f
        return draft
    }

    private func update(_ draft: TargetsDraft) -> TargetsUpdate? {
        if case .valid(let update) = draft.validate() { update } else { nil }
    }

    private func errors(_ draft: TargetsDraft) -> [TargetsDraft.Field: String]? {
        if case .invalid(let errors) = draft.validate() { errors } else { nil }
    }

    @Test("Blank fields are nil (clear); numbers pass through; a comma is a decimal point")
    func parsing() {
        #expect(update(draft()) == TargetsUpdate(kcal: nil, protein: nil, carbs: nil, fat: nil))
        #expect(update(draft("2000", "70,5", "250", "0")) == TargetsUpdate(kcal: 2000, protein: 70.5, carbs: 250, fat: 0))
    }

    @Test("Calories must be above 0 and at most 20000")
    func calories() {
        #expect(errors(draft("0"))?[.calories] == "Calories must be more than 0 and at most 20000.")
        #expect(errors(draft("20000.1"))?[.calories] == "Calories must be more than 0 and at most 20000.")
        #expect(update(draft("20000")) != nil)
        #expect(update(draft("0.5")) != nil)
    }

    @Test("Macros allow 0 and have their own maximum and message")
    func macros() {
        #expect(update(draft("", "0", "0", "0")) != nil)
        #expect(errors(draft("", "2001"))?[.protein] == "Protein must be at most 2000 g.")
        #expect(errors(draft("", "", "5001"))?[.carbs] == "Carbohydrates must be at most 5000 g.")
        #expect(errors(draft("", "", "", "2001"))?[.fat] == "Fat must be at most 2000 g.")
        #expect(update(draft("", "2000", "5000", "2000")) != nil)
    }

    @Test("Text that is not a plain number gets a hint with an example, and negatives count as not a number")
    func notANumber() {
        let result = errors(draft("abc", "-5", "1e3", "12x"))
        #expect(result?[.calories] == "Enter a number, for example 2000.")
        #expect(result?[.protein] == "Enter a number, for example 70.")
        #expect(result?[.carbs] == "Enter a number, for example 250.")
        #expect(result?[.fat] == "Enter a number, for example 70.")
    }

    @Test("Every bad field is reported at once")
    func allErrors() {
        #expect(errors(draft("0", "9999", "9999", "9999"))?.count == 4)
    }

    @Test("A draft built from a user shows plain numbers and blanks for unset targets")
    func fromUser() {
        let user = ProfileFixtures.user(kcal: 2000, protein: 70.5)
        let built = TargetsDraft(user: user)
        #expect(built.calories == "2000")
        #expect(built.protein == "70.5")
        #expect(built.carbs == "")
        #expect(built.fat == "")
        #expect(built == draft("2000", "70.5"))
    }

    @Test("A server field path maps to a draft field")
    func apiPaths() {
        #expect(TargetsDraft.Field(apiPath: "target_kcal") == .calories)
        #expect(TargetsDraft.Field(apiPath: "target_protein_g") == .protein)
        #expect(TargetsDraft.Field(apiPath: "target_carbs_g") == .carbs)
        #expect(TargetsDraft.Field(apiPath: "target_fat_g") == .fat)
        #expect(TargetsDraft.Field(apiPath: "display_name") == nil)
    }
}
