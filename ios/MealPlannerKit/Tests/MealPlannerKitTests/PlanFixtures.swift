import API
import Foundation

extension Fixtures {
    static func entry(
        id: String = UUID().uuidString, date: String = "2026-10-05",
        slot: Components.Schemas.Slot = .breakfast, mealID: String = "m1", mealName: String = "Oats", portion: Double = 1
    ) -> Components.Schemas.PlanEntry {
        .init(id: id, date: date, slot: slot, mealId: mealID, mealName: mealName, portion: portion, createdAt: date0, updatedAt: date0)
    }

    /// A day as `GET /plan` returns it. `calories` is the day's total (nil means unknown).
    static func day(_ date: String, entries: [Components.Schemas.PlanEntry] = [], calories: Double? = 0) -> Components.Schemas.DailyTotal {
        .init(date: date, entries: entries, nutritionPerDay: nutrients(calories: calories))
    }

    static func targets(
        kcal: Double? = 2000, protein: Double? = 100, carbs: Double? = 250, fat: Double? = 70
    ) -> Components.Schemas.Targets {
        .init(targetKcal: kcal, targetProteinG: protein, targetCarbsG: carbs, targetFatG: fat)
    }

    static func planRange(from: String, to: String, days: [Components.Schemas.DailyTotal], targets: Components.Schemas.Targets = targets()) -> String {
        json(Components.Schemas.PlanRange(from: from, to: to, days: days, targets: targets))
    }

    private static var date0: Date { date }
}
