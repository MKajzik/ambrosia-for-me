import API

/// The 18 nutrients the API tracks, in the order it lists them (`NutrientAmounts` in openapi.yaml).
public enum NutrientKey: String, CaseIterable, Sendable, Identifiable {
    case calories, protein, carbohydrates, sugar, fibre, fat, saturatedFat, sodium, potassium
    case calcium, iron, magnesium, zinc, vitaminA, vitaminC, vitaminD, vitaminB12, folate

    public var id: String { rawValue }

    public func amount(in n: Components.Schemas.NutrientAmounts) -> Double? {
        switch self {
        case .calories: n.calories
        case .protein: n.protein
        case .carbohydrates: n.carbohydrates
        case .sugar: n.sugar
        case .fibre: n.fibre
        case .fat: n.fat
        case .saturatedFat: n.saturatedFat
        case .sodium: n.sodium
        case .potassium: n.potassium
        case .calcium: n.calcium
        case .iron: n.iron
        case .magnesium: n.magnesium
        case .zinc: n.zinc
        case .vitaminA: n.vitaminA
        case .vitaminC: n.vitaminC
        case .vitaminD: n.vitaminD
        case .vitaminB12: n.vitaminB12
        case .folate: n.folate
        }
    }
}

public enum NutrientUnit: String, Sendable {
    case kcal, g, mg
    case microgram = "µg"
}

public enum NutrientGroup: String, CaseIterable, Sendable {
    case macronutrients = "Macronutrients"
    case minerals = "Minerals"
    case vitamins = "Vitamins"
}

public struct NutrientInfo: Sendable, Identifiable {
    public let key: NutrientKey
    public let label: String
    public let unit: NutrientUnit
    public let group: NutrientGroup
    public var id: String { key.rawValue }
}

public enum NutrientCatalog {
    public static let all: [NutrientInfo] = [
        .init(key: .calories, label: "Calories", unit: .kcal, group: .macronutrients),
        .init(key: .protein, label: "Protein", unit: .g, group: .macronutrients),
        .init(key: .carbohydrates, label: "Carbohydrates", unit: .g, group: .macronutrients),
        .init(key: .sugar, label: "Sugar", unit: .g, group: .macronutrients),
        .init(key: .fibre, label: "Fibre", unit: .g, group: .macronutrients),
        .init(key: .fat, label: "Fat", unit: .g, group: .macronutrients),
        .init(key: .saturatedFat, label: "Saturated fat", unit: .g, group: .macronutrients),
        .init(key: .sodium, label: "Sodium", unit: .mg, group: .minerals),
        .init(key: .potassium, label: "Potassium", unit: .mg, group: .minerals),
        .init(key: .calcium, label: "Calcium", unit: .mg, group: .minerals),
        .init(key: .iron, label: "Iron", unit: .mg, group: .minerals),
        .init(key: .magnesium, label: "Magnesium", unit: .mg, group: .minerals),
        .init(key: .zinc, label: "Zinc", unit: .mg, group: .minerals),
        .init(key: .vitaminA, label: "Vitamin A", unit: .microgram, group: .vitamins),
        .init(key: .vitaminC, label: "Vitamin C", unit: .mg, group: .vitamins),
        .init(key: .vitaminD, label: "Vitamin D", unit: .microgram, group: .vitamins),
        .init(key: .vitaminB12, label: "Vitamin B12", unit: .microgram, group: .vitamins),
        .init(key: .folate, label: "Folate", unit: .microgram, group: .vitamins),
    ]

    /// The summary tier: each keeps one colour across the product.
    public static let macroKeys: [NutrientKey] = [.calories, .protein, .carbohydrates, .fat]

    private static let byKey = Dictionary(uniqueKeysWithValues: all.map { ($0.key, $0) })

    /// Total: the catalog test pins that every key has an entry.
    public static func info(for key: NutrientKey) -> NutrientInfo { byKey[key]! }
}

/// FDA Daily Values for adults, in the catalog's own unit for each nutrient (as web's `daily-values.ts`).
/// Only nutrients a food label gives a percentage for; macros are judged against personal targets elsewhere.
public enum DailyValues {
    private static let values: [NutrientKey: Double] = [
        .fibre: 28, .saturatedFat: 20, .sodium: 2300, .potassium: 4700, .calcium: 1300, .iron: 18,
        .magnesium: 420, .zinc: 11, .vitaminA: 900, .vitaminC: 90, .vitaminD: 20, .vitaminB12: 2.4, .folate: 400,
    ]

    public static func value(for key: NutrientKey) -> Double? { values[key] }

    /// The amount as a percentage of the Daily Value, or nil when the amount is unknown or there is no reference.
    public static func percent(_ key: NutrientKey, amount: Double?) -> Double? {
        guard let reference = values[key], let amount else { return nil }
        return amount / reference * 100
    }
}
