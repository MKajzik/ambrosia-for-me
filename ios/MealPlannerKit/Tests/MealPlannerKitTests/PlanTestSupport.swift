import API
import Foundation
@testable import Features

/// A tiny in-memory stand-in for the plan endpoints, so view-model tests read like the real round trip: the "server"
/// computes each day's calories (100 per portion), never the code under test.
final class PlanServer: @unchecked Sendable {
    struct Slot: Sendable {
        let dayIndex: Int
        let slot: Components.Schemas.Slot
        let mealID: String
        let portion: Double
    }

    struct Template: Sendable {
        let dayCount: Int
        let slots: [Slot]
    }

    private let lock = NSLock()
    private var byDate: [String: [Components.Schemas.PlanEntry]] = [:]
    private var templates: [String: Template]
    private let mealNames: [String: String]
    private var putGate: Gate?
    private var failPUT = false
    private var failGET = false
    private let days = LocalDay(timeZone: TimeZone(identifier: "UTC")!)

    init(
        entries: [Components.Schemas.PlanEntry] = [],
        templates: [String: Template] = [:],
        mealNames: [String: String] = ["m1": "Oats", "m2": "Rice"]
    ) {
        self.templates = templates
        self.mealNames = mealNames
        for entry in entries { byDate[entry.date, default: []].append(entry) }
    }

    func setPutGate(_ gate: Gate?) { lock.lock(); putGate = gate; lock.unlock() }
    func setFailPUT(_ value: Bool) { lock.lock(); failPUT = value; lock.unlock() }
    func setFailGET(_ value: Bool) { lock.lock(); failGET = value; lock.unlock() }

    func entries(on date: String) -> [Components.Schemas.PlanEntry] {
        lock.lock(); defer { lock.unlock() }
        return byDate[date] ?? []
    }

    func route(_ call: RoutingTransport.Call) async throws -> (status: Int, body: String) {
        let parts = call.route.split(separator: " ", maxSplits: 1).map(String.init)
        let segments = parts[1].split(separator: "/").map(String.init)
        if parts[0] == "GET", segments == ["plan"] { return try getPlan(call.path) }
        if parts[0] == "PUT", segments.count == 3, segments[0] == "plan" { return await putEntry(segments[1], segments[2], call.body) }
        if parts[0] == "DELETE", segments.count == 3, segments[0] == "plan" { return deleteEntry(segments[1], segments[2]) }
        if parts[0] == "POST", segments.count == 3, segments[0] == "diet-templates", segments[2] == "apply" { return apply(segments[1], call.body) }
        return (500, Fixtures.problem(500, code: "unrouted"))
    }

    private func getPlan(_ path: String) throws -> (status: Int, body: String) {
        lock.lock(); defer { lock.unlock() }
        if failGET { throw URLError(.notConnectedToInternet) }
        let items = URLComponents(string: "http://x" + path)?.queryItems ?? []
        guard let from = items.first(where: { $0.name == "from" })?.value, let to = items.first(where: { $0.name == "to" })?.value else {
            return (400, Fixtures.problem(400, code: "validation_failed"))
        }
        var result: [Components.Schemas.DailyTotal] = []
        var date = from
        while date <= to, result.count < 92 {
            let entries = (byDate[date] ?? []).sorted { Self.rank($0.slot) < Self.rank($1.slot) }
            let calories = entries.reduce(0.0) { $0 + $1.portion * 100 }
            result.append(Fixtures.day(date, entries: entries, calories: calories))
            date = days.addDays(date, 1)
        }
        return (200, Fixtures.planRange(from: from, to: to, days: result))
    }

    /// `NSLock` cannot be taken in an async function under Swift 6, so every locked region is its own sync method.
    private func putSettings() -> (gate: Gate?, failing: Bool) {
        lock.lock(); defer { lock.unlock() }
        return (putGate, failPUT)
    }

    private func putEntry(_ date: String, _ slotName: String, _ body: String) async -> (status: Int, body: String) {
        let settings = putSettings()
        if let gate = settings.gate { await gate.wait() }
        if settings.failing { return (500, Fixtures.problem(500, code: "internal")) }
        return storeEntry(date, slotName, body)
    }

    private func storeEntry(_ date: String, _ slotName: String, _ body: String) -> (status: Int, body: String) {
        guard let slot = Components.Schemas.Slot(rawValue: slotName),
              let object = (try? JSONSerialization.jsonObject(with: Data(body.utf8))) as? [String: Any],
              let mealID = object["meal_id"] as? String
        else { return (400, Fixtures.problem(400, code: "validation_failed")) }
        let portion = (object["portion"] as? Double) ?? 1
        let entry = Fixtures.entry(date: date, slot: slot, mealID: mealID, mealName: mealNames[mealID] ?? mealID, portion: portion)
        lock.lock(); defer { lock.unlock() }
        var list = byDate[date] ?? []
        if slot != .snack { list.removeAll { $0.slot == slot } }
        list.append(entry)
        byDate[date] = list
        return (200, Fixtures.json(entry))
    }

    private func deleteEntry(_ date: String, _ slotName: String) -> (status: Int, body: String) {
        guard let slot = Components.Schemas.Slot(rawValue: slotName) else { return (400, Fixtures.problem(400, code: "validation_failed")) }
        lock.lock(); defer { lock.unlock() }
        let before = byDate[date]?.count ?? 0
        byDate[date]?.removeAll { $0.slot == slot }
        return (byDate[date]?.count ?? 0) < before ? (204, "") : (404, Fixtures.problem(404, code: "not_found"))
    }

    private func apply(_ id: String, _ body: String) -> (status: Int, body: String) {
        lock.lock(); defer { lock.unlock() }
        guard let template = templates[id] else { return (404, Fixtures.problem(404, code: "not_found")) }
        guard let object = (try? JSONSerialization.jsonObject(with: Data(body.utf8))) as? [String: Any],
              let start = object["start_date"] as? String
        else { return (400, Fixtures.problem(400, code: "validation_failed")) }
        let overwrite = (object["overwrite"] as? Bool) ?? false
        let conflict = template.slots.contains { slot in
            slot.slot != .snack && (byDate[days.addDays(start, slot.dayIndex)] ?? []).contains { $0.slot == slot.slot }
        }
        if conflict, !overwrite { return (409, Fixtures.problem(409, code: "plan_conflict")) }
        for slot in template.slots {
            let date = days.addDays(start, slot.dayIndex)
            var list = byDate[date] ?? []
            if slot.slot != .snack { list.removeAll { $0.slot == slot.slot } }
            list.append(Fixtures.entry(date: date, slot: slot.slot, mealID: slot.mealID, mealName: mealNames[slot.mealID] ?? slot.mealID, portion: slot.portion))
            byDate[date] = list
        }
        return (204, "")
    }

    private static func rank(_ slot: Components.Schemas.Slot) -> Int {
        switch slot {
        case .breakfast: 0
        case .lunch: 1
        case .dinner: 2
        case .snack: 3
        }
    }
}
