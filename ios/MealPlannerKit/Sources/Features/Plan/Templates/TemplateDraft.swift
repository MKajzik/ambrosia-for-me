import API
import Foundation

/// The editable text of a diet template, with validation and the diff against what the server holds (the template
/// counterpart of `MealDraft`). Slots are kept and saved in a canonical order (day, then breakfast, lunch, dinner,
/// snack, then insertion order) on both sides of the diff, so the server's ordering never reads as an edit.
public struct TemplateDraft: Equatable, Sendable {
    public typealias Slot = Components.Schemas.Slot

    public static let slotOrder: [Slot] = [.breakfast, .lunch, .dinner, .snack]

    public struct Row: Equatable, Identifiable, Sendable {
        public var id: String
        public var dayIndex: Int
        public var slot: Slot
        public var mealID: String
        public var mealName: String
        public var portion: String
    }

    public struct Valid: Equatable, Sendable {
        public var name: String
        public var shared: Bool
        public var slots: [Components.Schemas.TemplateSlotInput]
    }

    public struct Errors: Equatable, Sendable {
        public var name: String?
        public var rows: [String: String] = [:]
        var isEmpty: Bool { name == nil && rows.isEmpty }
    }

    public enum Validation: Equatable, Sendable {
        case valid(Valid)
        case invalid(Errors)
    }

    public struct Changes: Equatable, Sendable {
        public var patch: Components.Schemas.UpdateDietTemplateRequest?
        public var slots: [Components.Schemas.TemplateSlotInput]?
        public var isEmpty: Bool { patch == nil && slots == nil }
    }

    public var name = ""
    public var shared = false
    public var dayCount = 1
    public var rows: [Row] = []

    public init(name: String = "", shared: Bool = false, dayCount: Int = 1, rows: [Row] = []) {
        self.name = name
        self.shared = shared
        self.dayCount = dayCount
        self.rows = rows
    }

    public init(template: Components.Schemas.DietTemplate) {
        let rows = Self.canonical(template.slots, day: \.dayIndex, slot: \.slot).map {
            Row(id: $0.id, dayIndex: $0.dayIndex, slot: $0.slot, mealID: $0.mealId, mealName: $0.mealName, portion: plainNumber($0.portion))
        }
        self.init(name: template.name, shared: template.sharedWithPartner, dayCount: template.dayCount, rows: rows)
    }

    /// What the server holds, in the shape validation produces, so the two can be compared.
    public static func saved(from template: Components.Schemas.DietTemplate) -> Valid {
        Valid(
            name: template.name,
            shared: template.sharedWithPartner,
            slots: canonical(template.slots, day: \.dayIndex, slot: \.slot).map {
                .init(dayIndex: $0.dayIndex, slot: $0.slot, mealId: $0.mealId, portion: $0.portion)
            }
        )
    }

    public func rows(day: Int, slot: Slot) -> [Row] {
        rows.filter { $0.dayIndex == day && $0.slot == slot }
    }

    /// Choosing a meal for an occupied breakfast, lunch or dinner replaces it (keeping its portion); a snack is added.
    public mutating func setMeal(dayIndex: Int, slot: Slot, mealID: String, mealName: String) {
        if slot != .snack, let index = rows.firstIndex(where: { $0.dayIndex == dayIndex && $0.slot == slot }) {
            rows[index].mealID = mealID
            rows[index].mealName = mealName
        } else {
            rows.append(Row(id: UUID().uuidString, dayIndex: dayIndex, slot: slot, mealID: mealID, mealName: mealName, portion: "1"))
        }
    }

    public mutating func removeRow(id: String) {
        rows.removeAll { $0.id == id }
    }

    public static func nameError(_ raw: String) -> String? {
        let name = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        if name.isEmpty { return "Give the template a name." }
        if name.count > 200 { return "Use at most 200 characters." }
        return nil
    }

    public func validate() -> Validation {
        var errors = Errors()
        errors.name = Self.nameError(name)
        var portions: [String: Double] = [:]
        for row in rows {
            if row.dayIndex < 0 || row.dayIndex >= dayCount {
                errors.rows[row.id] = "That day is outside the template."
                continue
            }
            switch parseDecimal(row.portion) {
            case .value(let portion) where portion > 0 && portion <= 100:
                portions[row.id] = portion
            case .value:
                errors.rows[row.id] = "The portion must be more than 0 and at most 100."
            case .blank, .invalid:
                errors.rows[row.id] = "Enter a portion."
            }
        }
        guard errors.isEmpty else { return .invalid(errors) }
        return .valid(Valid(
            name: name.trimmingCharacters(in: .whitespacesAndNewlines),
            shared: shared,
            slots: Self.canonical(rows, day: \.dayIndex, slot: \.slot).map {
                .init(dayIndex: $0.dayIndex, slot: $0.slot, mealId: $0.mealID, portion: portions[$0.id])
            }
        ))
    }

    /// A patch of changed name and sharing, and the whole slot list when any slot changed (an empty list clears
    /// every slot, so removing the last slot still writes).
    public static func changes(from saved: Valid, to next: Valid) -> Changes {
        var patch = Components.Schemas.UpdateDietTemplateRequest()
        var changed = false
        if next.name != saved.name { patch.name = next.name; changed = true }
        if next.shared != saved.shared { patch.sharedWithPartner = next.shared; changed = true }
        return Changes(patch: changed ? patch : nil, slots: next.slots == saved.slots ? nil : next.slots)
    }

    /// By day, then breakfast, lunch, dinner, snack, then the order the items were in.
    private static func canonical<T>(_ items: [T], day: KeyPath<T, Int>, slot: KeyPath<T, Slot>) -> [T] {
        func rank(_ slot: Slot) -> Int { slotOrder.firstIndex(of: slot) ?? slotOrder.count }
        return items.enumerated().sorted { left, right in
            let (l, r) = (left.element, right.element)
            if l[keyPath: day] != r[keyPath: day] { return l[keyPath: day] < r[keyPath: day] }
            if rank(l[keyPath: slot]) != rank(r[keyPath: slot]) { return rank(l[keyPath: slot]) < rank(r[keyPath: slot]) }
            return left.offset < right.offset
        }.map(\.element)
    }
}
