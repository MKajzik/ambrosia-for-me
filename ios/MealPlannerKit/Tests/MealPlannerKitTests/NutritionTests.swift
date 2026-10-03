import API
import Testing
@testable import Features

@Suite
struct NutritionTests {
    static let apiOrder = [
        "calories", "protein", "carbohydrates", "sugar", "fibre", "fat", "saturatedFat", "sodium",
        "potassium", "calcium", "iron", "magnesium", "zinc", "vitaminA", "vitaminC", "vitaminD",
        "vitaminB12", "folate",
    ]

    @Test("The catalog lists the 18 nutrients once each, in the API's order")
    func catalogOrder() {
        #expect(NutrientCatalog.all.map(\.key.rawValue) == Self.apiOrder)
        #expect(Set(NutrientCatalog.all.map(\.key)).count == 18)
        #expect(NutrientKey.allCases.count == 18)
        for key in NutrientKey.allCases { _ = NutrientCatalog.info(for: key) }
    }

    @Test("Macro keys are in the catalog's Macronutrients group")
    func macros() {
        #expect(NutrientCatalog.macroKeys == [.calories, .protein, .carbohydrates, .fat])
        for key in NutrientCatalog.macroKeys { #expect(NutrientCatalog.info(for: key).group == .macronutrients) }
    }

    @Test("Units are the API's")
    func units() {
        #expect(NutrientCatalog.info(for: .calories).unit == .kcal)
        #expect(NutrientCatalog.info(for: .protein).unit == .g)
        #expect(NutrientCatalog.info(for: .sodium).unit == .mg)
        #expect(NutrientCatalog.info(for: .vitaminC).unit == .mg)
        #expect(NutrientCatalog.info(for: .vitaminD).unit == .microgram)
        #expect(NutrientCatalog.info(for: .folate).unit == .microgram)
    }

    @Test("amount(in:) reads every nutrient from the generated type")
    func amountAccessor() {
        let n = Components.Schemas.NutrientAmounts(
            calories: 1, protein: 2, carbohydrates: 3, sugar: 4, fibre: 5, fat: 6, saturatedFat: 7,
            sodium: 8, potassium: 9, calcium: 10, iron: 11, magnesium: 12, zinc: 13, vitaminA: 14,
            vitaminC: 15, vitaminD: 16, vitaminB12: 17, folate: 18
        )
        #expect(NutrientCatalog.all.map { $0.key.amount(in: n) } == (1...18).map(Double.init))
        #expect(NutrientKey.calories.amount(in: .init()) == nil)
    }

    @Test("Daily Values exist only for the nutrients a food label gives a percentage for")
    func dailyValueKeys() {
        let keys = NutrientKey.allCases.filter { DailyValues.value(for: $0) != nil }.map(\.rawValue)
        #expect(Set(keys) == ["calcium", "fibre", "folate", "iron", "magnesium", "potassium", "saturatedFat", "sodium", "vitaminA", "vitaminB12", "vitaminC", "vitaminD", "zinc"])
        #expect(DailyValues.value(for: .protein) == nil)
    }

    @Test("A Daily Value is 100% at its own reference amount, scales linearly, and has no percent when unknown")
    func percents() {
        for key in NutrientKey.allCases {
            if let reference = DailyValues.value(for: key) {
                #expect(abs((DailyValues.percent(key, amount: reference) ?? 0) - 100) < 1e-9)
            }
        }
        #expect(abs((DailyValues.percent(.sodium, amount: 1150) ?? 0) - 50) < 1e-9)
        #expect(DailyValues.percent(.vitaminB12, amount: 0) == 0)
        #expect(DailyValues.percent(.sodium, amount: nil) == nil)
        #expect(DailyValues.percent(.protein, amount: 20) == nil)
    }

    @Test(arguments: [
        (152.04, NutrientUnit.kcal, "152 kcal"), (5.2, .g, "5.2 g"), (10.4, .g, "10 g"),
        (2300.0, .mg, "2,300 mg"), (2.4, .microgram, "2.4 µg"), (0.0, .g, "0 g"), (0.049, .g, "0 g"),
        (1234567.0, .kcal, "1,234,567 kcal"), (5.0, .g, "5 g"),
    ])
    func formatsAmounts(amount: Double, unit: NutrientUnit, text: String) {
        #expect(NutritionFormat.amount(amount, unit: unit) == text)
    }

    @Test("An unknown amount is a dash, never zero")
    func unknownIsNotZero() {
        #expect(NutritionFormat.amount(nil, unit: .kcal) == NutritionFormat.noData)
        #expect(NutritionFormat.noData == "—")
        #expect(!NutritionFormat.amount(nil, unit: .g).contains("0"))
    }

    @Test("Daily Value percent rounds to a whole percent and groups thousands")
    func dailyValueText() {
        #expect(NutritionFormat.dailyValue(.sodium, amount: 1150) == "50%")
        #expect(NutritionFormat.dailyValue(.vitaminC, amount: 45.4) == "50%")
        #expect(NutritionFormat.dailyValue(.calcium, amount: 0) == "0%")
        #expect(NutritionFormat.dailyValue(.sodium, amount: 27600) == "1,200%")
        #expect(NutritionFormat.dailyValue(.iron, amount: nil) == NutritionFormat.noData)
    }

    @Test("Spoken amounts use unit words, and unknown reads as no data")
    func spoken() {
        #expect(NutritionFormat.spoken(420, unit: .kcal) == "420 kilocalories")
        #expect(NutritionFormat.spoken(5.2, unit: .g) == "5.2 grams")
        #expect(NutritionFormat.spoken(2.4, unit: .microgram) == "2.4 micrograms")
        #expect(NutritionFormat.spoken(nil, unit: .kcal) == "no data")
    }
    @Test("Ring speech says amount, target and percent; no target and unknown are spoken plainly")
    func ringSpoken() {
        #expect(NutritionFormat.ringSpoken(amount: 520, target: 2000, unit: .kcal) == "520 of 2,000 kilocalories, 26 percent")
        #expect(NutritionFormat.ringSpoken(amount: 2500, target: 2000, unit: .kcal) == "2,500 of 2,000 kilocalories, 125 percent")
        #expect(NutritionFormat.ringSpoken(amount: 520, target: nil, unit: .kcal) == "520 kilocalories, no target")
        #expect(NutritionFormat.ringSpoken(amount: 520, target: 0, unit: .kcal) == "520 kilocalories, no target")
        #expect(NutritionFormat.ringSpoken(amount: nil, target: 2000, unit: .kcal) == "no data")
        #expect(NutritionFormat.ringSpoken(amount: 38, target: 100, unit: .g) == "38 of 100 grams, 38 percent")
    }
}
