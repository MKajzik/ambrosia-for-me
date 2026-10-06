import Foundation
import Testing
@testable import Features

@Suite
struct LocalDayTests {
    private let utc = TimeZone(identifier: "UTC")!
    private func day(_ zone: TimeZone? = nil, now: Date = Date(timeIntervalSince1970: 1_790_000_000)) -> LocalDay {
        LocalDay(timeZone: zone ?? utc, now: { now })
    }

    @Test("addDays crosses month, year and leap-day boundaries")
    func addDays() {
        let d = day()
        #expect(d.addDays("2026-10-31", 1) == "2026-11-01")
        #expect(d.addDays("2026-12-31", 1) == "2027-01-01")
        #expect(d.addDays("2028-02-28", 1) == "2028-02-29")
        #expect(d.addDays("2026-03-01", -1) == "2026-02-28")
        #expect(d.addDays("2026-10-05", 0) == "2026-10-05")
        #expect(d.addDays("2026-10-05", 30) == "2026-11-04")
    }

    @Test("An unparseable day comes back unchanged")
    func unparseable() {
        #expect(day().addDays("nonsense", 1) == "nonsense")
        #expect(day().startOfWeek("2026-13-45") == "2026-13-45")
    }

    @Test("Weeks start on Monday, for every weekday")
    func startOfWeek() {
        let d = day()
        // 2026-10-05 is a Monday.
        for offset in 0...6 {
            #expect(d.startOfWeek(d.addDays("2026-10-05", offset)) == "2026-10-05", "offset \(offset)")
        }
        #expect(d.startOfWeek("2026-10-12") == "2026-10-12")
    }

    @Test("weekDays is seven consecutive days")
    func weekDays() {
        #expect(day().weekDays(startingAt: "2026-10-26") == [
            "2026-10-26", "2026-10-27", "2026-10-28", "2026-10-29", "2026-10-30", "2026-10-31", "2026-11-01",
        ])
    }

    @Test("A week stays seven days across a daylight-saving change, in both directions")
    func daylightSaving() {
        let ny = TimeZone(identifier: "America/New_York")!
        let d = day(ny)
        // Spring forward 2026-03-08, fall back 2026-11-01.
        #expect(d.weekDays(startingAt: "2026-03-02") == [
            "2026-03-02", "2026-03-03", "2026-03-04", "2026-03-05", "2026-03-06", "2026-03-07", "2026-03-08",
        ])
        #expect(d.addDays("2026-03-08", 1) == "2026-03-09")
        #expect(d.addDays("2026-03-02", 7) == "2026-03-09")
        #expect(d.addDays("2026-11-01", 1) == "2026-11-02")
        #expect(d.weekDays(startingAt: "2026-10-26").last == "2026-11-01")
    }

    @Test("today() is the local calendar day, not the UTC one")
    func todayIsLocal() {
        let formatter = ISO8601DateFormatter()
        let instant = formatter.date(from: "2026-10-05T22:30:00Z")!
        #expect(day(TimeZone(identifier: "UTC"), now: instant).today() == "2026-10-05")
        #expect(day(TimeZone(identifier: "Pacific/Auckland"), now: instant).today() == "2026-10-06")
        #expect(day(TimeZone(identifier: "America/Los_Angeles"), now: instant).today() == "2026-10-05")
        let lateEvening = formatter.date(from: "2026-10-06T06:30:00Z")!
        #expect(day(TimeZone(identifier: "America/Los_Angeles"), now: lateEvening).today() == "2026-10-05")
    }

    @Test("A date picker's Date and a day string convert both ways in the local zone")
    func conversions() {
        let ny = TimeZone(identifier: "America/New_York")!
        let d = day(ny)
        let date = d.date("2026-10-05")
        #expect(date != nil)
        #expect(d.day(from: date ?? Date()) == "2026-10-05")
        #expect(d.date("nonsense") == nil)
        // 23:30 New York time is still the 5th there, though it is the 6th in UTC.
        let late = ISO8601DateFormatter().date(from: "2026-10-06T03:30:00Z")!
        #expect(d.day(from: late) == "2026-10-05")
    }

    @Test("Headings and the week range, fixed en-US")
    func formatting() {
        let d = day()
        #expect(d.longDate("2026-10-05") == "Monday, October 5")
        #expect(d.heading("2026-10-05") == "Mon, Oct 5")
        #expect(d.weekRange(startingAt: "2026-10-05") == "Oct 5 – 11")
        #expect(d.weekRange(startingAt: "2026-10-26") == "Oct 26 – Nov 1")
        #expect(d.weekRange(startingAt: "2026-12-28") == "Dec 28 – Jan 3")
    }
}
