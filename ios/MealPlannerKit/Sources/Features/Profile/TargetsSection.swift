import SwiftUI

struct TargetsSection: View {
    @Bindable var viewModel: ProfileViewModel

    var body: some View {
        Section {
            field("Calories (kcal)", text: $viewModel.draft.calories, field: .calories, id: "targetCaloriesField")
            field("Protein (g)", text: $viewModel.draft.protein, field: .protein, id: "targetProteinField")
            field("Carbohydrates (g)", text: $viewModel.draft.carbs, field: .carbs, id: "targetCarbsField")
            field("Fat (g)", text: $viewModel.draft.fat, field: .fat, id: "targetFatField")
            if let banner = viewModel.banner {
                Text(banner).font(.footnote).foregroundStyle(.red).accessibilityIdentifier("targetsBanner")
            }
            Button("Save targets") { Task { await viewModel.saveTargets() } }
                .disabled(!viewModel.canSave)
                .accessibilityIdentifier("targetsSaveButton")
        } header: {
            Text("Daily targets")
        } footer: {
            Text("Leave a field blank for no target.")
        }
    }

    private func field(_ title: String, text: Binding<String>, field: TargetsDraft.Field, id: String) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            TextField(title, text: text).decimalKeyboard().accessibilityIdentifier(id)
            if let message = viewModel.fieldErrors[field] {
                Text(message).font(.caption).foregroundStyle(.red)
            }
        }
    }
}
