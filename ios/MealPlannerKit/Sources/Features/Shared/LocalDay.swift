import Foundation

/// The device's local calendar day as `YYYY-MM-DD` (the API's `date`), never UTC, with weeks starting Monday.
/// Arithmetic goes through `Calendar.date(byAdding: .day)` on noon of the day, so a week is seven days across a
/// daylight-saving change. Text is fixed en-US (as web). An unparseable day is returned unchanged.
/// The default zone tracks the device (`.autoupdatingCurrent`), so Today follows a traveller across time zones: `.current`
/// is a snapshot taken when the value is made.
public struct LocalDay: Sendable {
    private let timeZone: TimeZone
    private let clock: @Sendable () -> Date

    public init(timeZone: TimeZone = .autoupdatingCurrent, now: @escaping @Sendable () -> Date = { Date() }) {
        self.timeZone = timeZone
        self.clock = now
    }

    private var calendar: Calendar {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = timeZone
        calendar.locale = Locale(identifier: "en_US")
        return calendar
    }

    public func today() -> String { string(from: clock()) }

    /// The day as a `Date` (noon local), for a date picker; `nil` if unparseable.
    public func date(_ day: String) -> Date? { date(from: day) }

    /// The local calendar day of a `Date`, e.g. what a date picker produced.
    public func day(from date: Date) -> String { string(from: date) }

    public func addDays(_ day: String, _ count: Int) -> String {
        guard let date = date(from: day), let moved = calendar.date(byAdding: .day, value: count, to: date) else { return day }
        return string(from: moved)
    }

    /// The Monday of the week containing `day`.
    public func startOfWeek(_ day: String) -> String {
        guard let date = date(from: day) else { return day }
        let weekday = calendar.component(.weekday, from: date) // 1 = Sunday, 2 = Monday
        return addDays(day, -((weekday + 5) % 7))
    }

    public func weekDays(startingAt monday: String) -> [String] {
        (0..<7).map { addDays(monday, $0) }
    }

    /// "Monday, October 5"
    public func longDate(_ day: String) -> String { format(day, "EEEE, MMMM d") }

    /// "Mon, Oct 5"
    public func heading(_ day: String) -> String { format(day, "EEE, MMM d") }

    /// "Oct 5 – 11", or "Oct 26 – Nov 1" when the week spans two months.
    public func weekRange(startingAt monday: String) -> String {
        let sunday = addDays(monday, 6)
        guard let start = date(from: monday), let end = date(from: sunday) else { return monday }
        let sameMonth = calendar.component(.month, from: start) == calendar.component(.month, from: end)
        let endText = sameMonth ? format(sunday, "d") : format(sunday, "MMM d")
        return "\(format(monday, "MMM d")) – \(endText)"
    }

    private func format(_ day: String, _ pattern: String) -> String {
        guard let date = date(from: day) else { return day }
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US")
        formatter.calendar = calendar
        formatter.timeZone = timeZone
        formatter.dateFormat = pattern
        return formatter.string(from: date)
    }

    private func string(from date: Date) -> String {
        let parts = calendar.dateComponents([.year, .month, .day], from: date)
        return String(format: "%04d-%02d-%02d", parts.year ?? 0, parts.month ?? 0, parts.day ?? 0)
    }

    /// Noon of the day, so adding days never lands in a daylight-saving gap.
    private func date(from day: String) -> Date? {
        let parts = day.split(separator: "-").compactMap { Int($0) }
        guard parts.count == 3, day.split(separator: "-").map(\.count) == [4, 2, 2] else { return nil }
        var components = DateComponents()
        components.year = parts[0]
        components.month = parts[1]
        components.day = parts[2]
        components.hour = 12
        // `Calendar` rolls an impossible date (month 13, day 45) over instead of failing: reject those.
        guard let date = calendar.date(from: components) else { return nil }
        let check = calendar.dateComponents([.year, .month, .day], from: date)
        guard check.year == parts[0], check.month == parts[1], check.day == parts[2] else { return nil }
        return date
    }
}
