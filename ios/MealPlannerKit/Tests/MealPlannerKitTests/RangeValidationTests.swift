import Foundation
import Testing
@testable import Features

@Suite
struct RangeValidationTests {
    private let day = LocalDay(timeZone: TimeZone(identifier: "UTC")!)

    @Test("A one-day range and a 92-day range are valid")
    func valid() {
        #expect(RangeValidation.error(from: "2026-10-05", to: "2026-10-05", day: day) == nil)
        #expect(RangeValidation.error(from: "2026-01-01", to: "2026-04-02", day: day) == nil) // 92 days inclusive
    }

    @Test("93 days is refused, as is an end before the start or a malformed date")
    func invalid() {
        #expect(RangeValidation.error(from: "2026-01-01", to: "2026-04-03", day: day) == "Pick at most 92 days.")
        #expect(RangeValidation.error(from: "2026-10-06", to: "2026-10-05", day: day) == "The end date must be on or after the start date.")
        #expect(RangeValidation.error(from: "", to: "2026-10-05", day: day) == "Choose a start and end date.")
        #expect(RangeValidation.error(from: "2026-13-40", to: "2026-10-05", day: day) == "Choose a start and end date.")
    }
}
