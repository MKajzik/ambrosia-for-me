import API
import Testing
@testable import Features

@Suite
struct NutrientMathTests {
    private func n(_ calories: Double?, protein: Double? = 0) -> Components.Schemas.NutrientAmounts {
        Fixtures.nutrients(calories: calories, protein: protein)
    }

    @Test("The sum of nothing is zero for every nutrient")
    func emptySum() {
        let sum = NutrientMath.sum([])
        #expect(NutrientKey.allCases.allSatisfy { $0.amount(in: sum) == 0 })
    }

    @Test("Adds nutrient by nutrient")
    func adds() {
        let sum = NutrientMath.sum([n(300, protein: 10), n(450.5, protein: 5)])
        #expect(sum.calories == 750.5)
        #expect(sum.protein == 15)
    }

    @Test("A nutrient unknown in any item is unknown in the sum; the rest stay known")
    func unknownPropagates() {
        let sum = NutrientMath.sum([n(300), n(nil), n(100)])
        #expect(sum.calories == nil)
        #expect(sum.protein == 0)
    }

    @Test("Scaling multiplies known amounts and leaves unknown ones unknown")
    func scales() {
        let scaled = NutrientMath.scaled(n(700, protein: nil), by: 1.0 / 7.0)
        #expect(abs((scaled.calories ?? 0) - 100) < 1e-9)
        #expect(scaled.protein == nil)
    }

    @Test("A week total needs all seven days: an unfetched date is absent, not empty")
    func weekTotalsNeedEveryDay() {
        let dates = ["a", "b", "c", "d", "e", "f", "g"]
        var days: [String: Components.Schemas.DailyTotal] = [:]
        for (i, date) in dates.enumerated() { days[date] = Fixtures.day(date, calories: Double(100 * (i + 1))) }
        let week = WeekTotals.make(days: days, dates: dates)
        #expect(week?.total.calories == 2800)
        #expect(week?.perDayAverage.calories == 400)

        days.removeValue(forKey: "d")
        #expect(WeekTotals.make(days: days, dates: dates) == nil)
        #expect(WeekTotals.make(days: [:], dates: dates) == nil)
    }

    @Test("A day with unknown calories makes the week's calories unknown")
    func weekTotalsUnknown() {
        let dates = ["a", "b"]
        let days = ["a": Fixtures.day("a", calories: 100), "b": Fixtures.day("b", calories: nil)]
        #expect(WeekTotals.make(days: days, dates: dates)?.total.calories == nil)
    }
}
