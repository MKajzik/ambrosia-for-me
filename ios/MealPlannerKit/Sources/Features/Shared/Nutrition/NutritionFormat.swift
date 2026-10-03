import Foundation

public enum NutritionFormat {
    /// What an unknown amount looks like. Never `0`: an unknown amount is not a zero amount.
    public static let noData = "—"

    /// No decimals for kcal or values of 10 and above, else up to one decimal; grouped, en-US (as web).
    public static func amount(_ amount: Double?, unit: NutrientUnit) -> String {
        guard let amount else { return noData }
        let digits = unit == .kcal || abs(amount) >= 10 ? 0 : 1
        return "\(enUS(amount, maxFractionDigits: digits)) \(unit.rawValue)"
    }

    public static func dailyValue(_ key: NutrientKey, amount: Double?) -> String {
        guard let percent = DailyValues.percent(key, amount: amount) else { return noData }
        return "\(enUS(percent.rounded(), maxFractionDigits: 0))%"
    }

    /// VoiceOver text: unit words instead of symbols, "no data" instead of a dash.
    public static func spoken(_ amount: Double?, unit: NutrientUnit) -> String {
        guard let amount else { return "no data" }
        let word: String
        switch unit {
        case .kcal: word = "kilocalories"
        case .g: word = "grams"
        case .mg: word = "milligrams"
        case .microgram: word = "micrograms"
        }
        return "\(enUS(amount, maxFractionDigits: unit == .kcal || abs(amount) >= 10 ? 0 : 1)) \(word)"
    }

    /// The unit word appears once, after the target ("520 of 2,000 kilocalories, 26 percent").
    public static func ringSpoken(amount: Double?, target: Double?, unit: NutrientUnit) -> String {
        guard let amount else { return "no data" }
        guard let progress = targetProgress(value: amount, target: target), let target else {
            return "\(spoken(amount, unit: unit)), no target"
        }
        let amountNumber = spoken(amount, unit: unit).split(separator: " ", maxSplits: 1).first.map(String.init) ?? ""
        return "\(amountNumber) of \(spoken(target, unit: unit)), \(progress.percent) percent"
    }

    private static func enUS(_ value: Double, maxFractionDigits: Int) -> String {
        let formatter = NumberFormatter()
        formatter.locale = Locale(identifier: "en_US")
        formatter.numberStyle = .decimal
        formatter.usesGroupingSeparator = true
        formatter.minimumFractionDigits = 0
        formatter.maximumFractionDigits = maxFractionDigits
        formatter.roundingMode = .halfUp
        return formatter.string(from: NSNumber(value: value)) ?? String(value)
    }
}
