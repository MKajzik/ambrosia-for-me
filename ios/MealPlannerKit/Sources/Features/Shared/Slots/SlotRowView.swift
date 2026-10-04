import API
import SwiftUI

/// One slot of one day (breakfast, lunch, dinner, or the snacks), shared by Today and Plan. Pure presentation: every
/// action is a closure, and `DaySlotsView` owns the sheets they open.
struct SlotRowView: View {
    let slot: Components.Schemas.Slot
    let entries: [Components.Schemas.PlanEntry]
    let isDisabled: Bool
    let onPick: () -> Void
    let onChangePortion: (Components.Schemas.PlanEntry) -> Void
    let onRemove: () -> Void
    let onClearSnacks: () -> Void

    private var title: String {
        switch slot {
        case .breakfast: "Breakfast"
        case .lunch: "Lunch"
        case .dinner: "Dinner"
        case .snack: "Snacks"
        }
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(title).font(.subheadline.weight(.semibold)).foregroundStyle(.secondary)
            if slot == .snack { snacks } else { single }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .disabled(isDisabled)
    }

    @ViewBuilder
    private var single: some View {
        if let entry = entries.first {
            HStack {
                Button(action: onPick) {
                    VStack(alignment: .leading, spacing: 2) {
                        Text(entry.mealName)
                        if entry.portion != 1 {
                            Text("× \(plainNumber(entry.portion))").font(.caption).foregroundStyle(.secondary)
                        }
                    }
                }
                .buttonStyle(.plain)
                .accessibilityIdentifier("slotMeal-\(slot.rawValue)")
                Spacer()
                Menu {
                    Button("Change portion") { onChangePortion(entry) }
                        .accessibilityIdentifier("changePortionButton")
                    Button("Remove", role: .destructive, action: onRemove)
                } label: {
                    Image(systemName: "ellipsis.circle")
                }
                .accessibilityLabel("\(title) options")
                .accessibilityIdentifier("slotMenu-\(slot.rawValue)")
            }
            .contextMenu {
                Button("Change portion") { onChangePortion(entry) }
                Button("Remove", role: .destructive, action: onRemove)
            }
        } else {
            Button("Add meal", action: onPick)
                .accessibilityIdentifier("slotAddButton-\(slot.rawValue)")
        }
    }

    /// A single snack cannot be edited or removed: the API addresses snacks only by date and slot, so the plan can
    /// only add one or clear them all.
    @ViewBuilder
    private var snacks: some View {
        ForEach(entries, id: \.id) { entry in
            HStack {
                Text(entry.mealName)
                if entry.portion != 1 {
                    Text("× \(plainNumber(entry.portion))").font(.caption).foregroundStyle(.secondary)
                }
            }
        }
        HStack {
            Button("Add snack", action: onPick)
                .accessibilityIdentifier("slotAddButton-snack")
            if !entries.isEmpty {
                Spacer()
                Button("Clear snacks", role: .destructive, action: onClearSnacks)
                    .accessibilityIdentifier("clearSnacksButton")
            }
        }
    }
}
