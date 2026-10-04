import API
import Repositories
import SwiftUI

/// The four slots of one day with their actions, as ONE grouped list row: a single set of sheets (picker, portion,
/// clear-snacks confirmation, error alert) serves the whole day. Used by Today and by each day of Plan.
struct DaySlotsView: View {
    let date: String
    let viewModel: PlanViewModel
    let meals: MealsRepository
    @State private var picking: SlotChoice?
    @State private var portionEdit: PortionEdit?
    @State private var confirmingClear = false
    @State private var errorText: String?

    struct SlotChoice: Identifiable {
        let slot: Components.Schemas.Slot
        var id: String { slot.rawValue }
    }

    struct PortionEdit: Identifiable {
        let entry: Components.Schemas.PlanEntry
        var id: String { entry.id }
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            ForEach(Components.Schemas.Slot.allCases, id: \.self) { slot in
                SlotRowView(
                    slot: slot,
                    entries: viewModel.entries(on: date, slot: slot),
                    isDisabled: viewModel.isWriting,
                    onPick: { picking = SlotChoice(slot: slot) },
                    onChangePortion: { portionEdit = PortionEdit(entry: $0) },
                    onRemove: { Task { errorText = await viewModel.remove(date: date, slot: slot) } },
                    onClearSnacks: { confirmingClear = true }
                )
            }
        }
        .sheet(item: $picking) { choice in
            MealPickerView(meals: meals, title: "\(label(choice.slot)) · \(viewModel.localDay.heading(date))") { meal in
                Task { errorText = await viewModel.setMeal(date: date, slot: choice.slot, mealID: meal.id, portion: 1) }
            }
        }
        .sheet(item: $portionEdit) { edit in
            PortionSheetView(mealName: edit.entry.mealName, initial: edit.entry.portion) { portion in
                await viewModel.setMeal(date: date, slot: edit.entry.slot, mealID: edit.entry.mealId, portion: portion)
            }
        }
        .confirmationDialog("Clear all snacks?", isPresented: $confirmingClear, titleVisibility: .visible) {
            Button("Clear snacks", role: .destructive) {
                Task { errorText = await viewModel.clearSnacks(date: date) }
            }
        } message: {
            Text("This removes every snack planned for \(viewModel.localDay.longDate(date)).")
        }
        .alert(
            "Couldn't complete that",
            isPresented: Binding(get: { errorText != nil }, set: { if !$0 { errorText = nil } })
        ) {
            Button("OK", role: .cancel) {}
        } message: {
            Text(errorText ?? "")
        }
    }

    private func label(_ slot: Components.Schemas.Slot) -> String {
        switch slot {
        case .breakfast: "Breakfast"
        case .lunch: "Lunch"
        case .dinner: "Dinner"
        case .snack: "Snack"
        }
    }
}
