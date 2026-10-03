import API

public extension Components.Schemas.NutrientAmounts {
    /// Builds the 18-nutrient value from one lookup per key.
    init(values: (NutrientKey) -> Double?) {
        self.init(
            calories: values(.calories), protein: values(.protein), carbohydrates: values(.carbohydrates),
            sugar: values(.sugar), fibre: values(.fibre), fat: values(.fat), saturatedFat: values(.saturatedFat),
            sodium: values(.sodium), potassium: values(.potassium), calcium: values(.calcium), iron: values(.iron),
            magnesium: values(.magnesium), zinc: values(.zinc), vitaminA: values(.vitaminA), vitaminC: values(.vitaminC),
            vitaminD: values(.vitaminD), vitaminB12: values(.vitaminB12), folate: values(.folate)
        )
    }
}

/// The only arithmetic the app does on nutrition: summing and scaling the server's own totals. A nutrient is unknown
/// (`nil`) in a sum if it is unknown in any item: unknown is never treated as zero.
public enum NutrientMath {
    public static func sum(_ list: [Components.Schemas.NutrientAmounts]) -> Components.Schemas.NutrientAmounts {
        .init { key in
            var total = 0.0
            for item in list {
                guard let amount = key.amount(in: item) else { return nil }
                total += amount
            }
            return total
        }
    }

    public static func scaled(_ nutrition: Components.Schemas.NutrientAmounts, by factor: Double) -> Components.Schemas.NutrientAmounts {
        .init { key in key.amount(in: nutrition).map { $0 * factor } }
    }
}

/// A week's total and per-day average from the server's per-day totals (the average is the total over seven days,
/// as web). Needs every date in the cache: a date that was never fetched is absent, not empty.
public struct WeekTotals: Equatable, Sendable {
    public let total: Components.Schemas.NutrientAmounts
    public let perDayAverage: Components.Schemas.NutrientAmounts

    public static func make(days: [String: Components.Schemas.DailyTotal], dates: [String]) -> WeekTotals? {
        let present = dates.compactMap { days[$0] }
        guard !dates.isEmpty, present.count == dates.count else { return nil }
        let total = NutrientMath.sum(present.map(\.nutritionPerDay))
        return WeekTotals(total: total, perDayAverage: NutrientMath.scaled(total, by: 1.0 / Double(dates.count)))
    }
}
