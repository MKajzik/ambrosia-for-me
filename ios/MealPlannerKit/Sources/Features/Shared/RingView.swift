import API
import SwiftUI

extension Components.Schemas.Targets {
    func target(for key: NutrientKey) -> Double? {
        switch key {
        case .calories: targetKcal
        case .protein: targetProteinG
        case .carbohydrates: targetCarbsG
        case .fat: targetFatG
        default: nil
        }
    }
}

/// One macro against its target. The ring fills to at most 100% but the number keeps the real percent (125% when
/// over). An unknown amount is "—" with an empty ring (never 0); a missing target shows the amount only.
struct RingView: View {
    let key: NutrientKey
    let amount: Double?
    let target: Double?
    var isDimmed = false
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    private var info: NutrientInfo { NutrientCatalog.info(for: key) }
    private var progress: TargetProgress? { targetProgress(value: amount, target: target) }

    var body: some View {
        VStack(spacing: 6) {
            ZStack {
                Circle().stroke(MacroStyle.color(for: key).opacity(0.2), lineWidth: 10)
                Circle()
                    .trim(from: 0, to: progress?.fraction ?? 0)
                    .stroke(MacroStyle.color(for: key), style: StrokeStyle(lineWidth: 10, lineCap: .round))
                    .rotationEffect(.degrees(-90))
                    .animation(reduceMotion ? nil : .default, value: progress?.fraction)
                VStack(spacing: 0) {
                    Text(NutritionFormat.amount(amount, unit: info.unit))
                        .font(.caption.weight(.semibold))
                        .monospacedDigit()
                        .minimumScaleFactor(0.7)
                        .lineLimit(1)
                    if let progress {
                        Text("\(progress.percent)%")
                            .font(.caption2)
                            .foregroundStyle(progress.over ? Color.red : Color.secondary)
                    }
                }
                .padding(.horizontal, 10)
            }
            .frame(width: 84, height: 84)
            Text(info.label).font(.caption).foregroundStyle(.secondary)
        }
        .opacity(isDimmed ? 0.5 : 1)
        // VoiceOver: "Calories, 520 of 2,000 kilocalories, 26 percent"; no target and unknown are spoken plainly.
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(info.label)
        .accessibilityValue(NutritionFormat.ringSpoken(amount: amount, target: target, unit: info.unit))
        .accessibilityIdentifier("ring-\(key.rawValue)")
    }
}
