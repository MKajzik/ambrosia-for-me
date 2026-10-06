import API
import SwiftUI

/// The week's total and per-day average, from the server's per-day totals. Shown only once all seven days are in
/// the cache (a date never fetched is absent, not empty).
struct WeekSummaryView: View {
    let totals: WeekTotals?

    var body: some View {
        if let totals {
            VStack(alignment: .leading, spacing: 8) {
                line("Week total", totals.total)
                line("Per day", totals.perDayAverage)
            }
        } else {
            Text("Loading the week…").foregroundStyle(.secondary)
        }
    }

    private func line(_ title: String, _ n: Components.Schemas.NutrientAmounts) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(title).font(.subheadline.weight(.semibold))
            Text(macroLine(n)).font(.footnote).foregroundStyle(.secondary).monospacedDigit()
        }
    }
}

/// "1,850 kcal · Protein 92 g · Carbs 210 g · Fat 60 g"; unknown amounts are "—".
func macroLine(_ n: Components.Schemas.NutrientAmounts) -> String {
    [
        NutritionFormat.amount(n.calories, unit: .kcal),
        "Protein \(NutritionFormat.amount(n.protein, unit: .g))",
        "Carbs \(NutritionFormat.amount(n.carbohydrates, unit: .g))",
        "Fat \(NutritionFormat.amount(n.fat, unit: .g))",
    ].joined(separator: " · ")
}
