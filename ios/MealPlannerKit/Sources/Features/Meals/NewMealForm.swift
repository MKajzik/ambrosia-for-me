import SwiftUI

/// Name and servings, both required by `createMeal`. On success the same sheet becomes the editor
/// (`MealsViewModel.createMeal` sets `presentation = .edit(id)`), as web opens the editor after `/meals/new`.
struct NewMealForm: View {
    let viewModel: MealsViewModel
    @State private var name = ""
    @State private var servings = "1"
    @State private var isCreating = false
    @State private var error: String?
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        Form {
            Section {
                TextField("Name", text: $name).accessibilityIdentifier("newMealNameField")
                TextField("Servings", text: $servings).decimalKeyboard().accessibilityIdentifier("newMealServingsField")
            }
            if let error {
                Section { Text(error).foregroundStyle(.red).accessibilityIdentifier("newMealError") }
            }
            Section {
                Button("Create meal") {
                    Task {
                        isCreating = true
                        error = await viewModel.createMeal(name: name, servings: servings)
                        isCreating = false
                    }
                }
                .disabled(isCreating)
                .accessibilityIdentifier("newMealCreateButton")
            }
        }
        .navigationTitle("New meal")
        .inlineNavigationTitle()
        .toolbar {
            ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
        }
    }
}
