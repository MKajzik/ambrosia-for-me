import Foundation

/// The date range a list is generated from, as web's `range.ts`: at most 92 days, both ends inclusive.
public enum RangeValidation {
    public static let maxDays = 92

    /// The message to show, or `nil` when the range is acceptable.
    public static func error(from: String, to: String, day: LocalDay) -> String? {
        guard day.date(from) != nil, day.date(to) != nil else { return "Choose a start and end date." }
        // ISO dates sort as text, so a plain comparison is a date comparison.
        if to < from { return "The end date must be on or after the start date." }
        if to > day.addDays(from, maxDays - 1) { return "Pick at most \(maxDays) days." }
        return nil
    }
}
