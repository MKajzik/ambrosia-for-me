import API
import Foundation

extension Fixtures {
    static func templateSlot(
        id: String = UUID().uuidString, dayIndex: Int = 0, slot: Components.Schemas.Slot = .breakfast,
        mealID: String = "m1", mealName: String = "Oats", portion: Double = 1
    ) -> Components.Schemas.TemplateSlot {
        .init(id: id, dayIndex: dayIndex, slot: slot, mealId: mealID, mealName: mealName, portion: portion)
    }

    static func template(
        id: String = "t1", name: String = "Cut week", dayCount: Int = 7, shared: Bool = false, isOwner: Bool = true,
        slots: [Components.Schemas.TemplateSlot] = []
    ) -> Components.Schemas.DietTemplate {
        .init(id: id, name: name, dayCount: dayCount, sharedWithPartner: shared, isOwner: isOwner, slots: slots,
              createdAt: date, updatedAt: date)
    }

    static func templateSummary(
        id: String = "t1", name: String = "Cut week", dayCount: Int = 7, shared: Bool = false
    ) -> Components.Schemas.DietTemplateSummary {
        .init(id: id, name: name, dayCount: dayCount, sharedWithPartner: shared, createdAt: date, updatedAt: date)
    }

    static func templateList(_ items: [Components.Schemas.DietTemplateSummary], next: String? = nil) -> String {
        json(Components.Schemas.DietTemplateList(items: items, nextCursor: next))
    }
}
