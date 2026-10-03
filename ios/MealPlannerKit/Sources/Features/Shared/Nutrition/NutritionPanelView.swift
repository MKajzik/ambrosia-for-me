import API
import SwiftUI

/// Each macro keeps one colour across the product (web uses teal, red, amber and blue tokens). System colours
/// adapt to dark mode and increased contrast on their own.
enum MacroStyle {
    static func color(for key: NutrientKey) -> Color {
        switch key {
        case .calories: .teal
        case .protein: .red
        case .carbohydrates: .orange
        case .fat: .blue
        default: .gray
        }
    }
}

/// "Per serving": four macro tiles, then an expandable "All nutrients" tier with micronutrients against FDA Daily
/// Values. Always shows the API's numbers; nothing is computed here.
public struct NutritionPanelView: View {
    private let nutrition: Components.Schemas.NutrientAmounts
    private let isDimmed: Bool
    @State private var showsAll = false
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    public init(nutrition: Components.Schemas.NutrientAmounts, isDimmed: Bool = false) {
        self.nutrition = nutrition
        self.isDimmed = isDimmed
    }

    private var hasUnknown: Bool {
        NutrientKey.allCases.contains { $0.amount(in: nutrition) == nil }
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Per serving").font(.headline)
            LazyVGrid(columns: [GridItem(.adaptive(minimum: 150), spacing: 10)], spacing: 10) {
                ForEach(NutrientCatalog.macroKeys) { key in
                    MacroTile(key: key, amount: key.amount(in: nutrition))
                }
            }
            if hasUnknown {
                Text("— means some ingredients lack data for that nutrient, so the total is unknown rather than zero.")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            DisclosureGroup("All nutrients", isExpanded: $showsAll) {
                allNutrients
            }
        }
        .opacity(isDimmed ? 0.5 : 1)
        .animation(reduceMotion ? nil : .default, value: isDimmed)
    }

    private var allNutrients: some View {
        VStack(alignment: .leading, spacing: 14) {
            ForEach(NutrientGroup.allCases, id: \.self) { group in
                VStack(alignment: .leading, spacing: 6) {
                    Text(group.rawValue).font(.subheadline.weight(.semibold))
                    ForEach(NutrientCatalog.all.filter { $0.group == group }) { info in
                        NutrientRow(info: info, amount: info.key.amount(in: nutrition))
                    }
                }
            }
            Text("Percentages are of the FDA Daily Value for adults.")
                .font(.footnote)
                .foregroundStyle(.secondary)
        }
        .padding(.top, 8)
    }
}

private struct MacroTile: View {
    let key: NutrientKey
    let amount: Double?

    var body: some View {
        let info = NutrientCatalog.info(for: key)
        VStack(alignment: .leading, spacing: 4) {
            Text(info.label).font(.footnote).foregroundStyle(.secondary)
            Text(NutritionFormat.amount(amount, unit: info.unit))
                .font(.title3.weight(.semibold))
                .monospacedDigit()
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(12)
        .background(MacroStyle.color(for: key).opacity(0.15), in: RoundedRectangle(cornerRadius: 12))
        .overlay(alignment: .leading) {
            RoundedRectangle(cornerRadius: 2)
                .fill(MacroStyle.color(for: key))
                .frame(width: 4)
                .padding(.vertical, 8)
        }
        // VoiceOver: "Calories, 420 kilocalories"; unknown reads "Calories, no data".
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(info.label)
        .accessibilityValue(NutritionFormat.spoken(amount, unit: info.unit))
        .accessibilityIdentifier("nutrientTile-\(key.rawValue)")
    }
}

private struct NutrientRow: View {
    let info: NutrientInfo
    let amount: Double?

    var body: some View {
        HStack {
            Text(info.label)
            Spacer()
            Text(NutritionFormat.amount(amount, unit: info.unit)).monospacedDigit()
            if DailyValues.value(for: info.key) != nil {
                Text(NutritionFormat.dailyValue(info.key, amount: amount))
                    .foregroundStyle(.secondary)
                    .monospacedDigit()
                    .frame(width: 56, alignment: .trailing)
            }
        }
        .font(.subheadline)
        .accessibilityElement(children: .combine)
    }
}
