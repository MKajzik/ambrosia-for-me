import API
import Repositories
import SwiftUI

/// The custom-ingredient form, as a sheet: creating one (reachable from search, over the search sheet) or editing one
/// of mine (from Profile's "My ingredients"). Editing shows the four main nutrients and keeps every other one.
struct CustomIngredientView: View {
    @State private var viewModel: CustomIngredientViewModel
    @Environment(\.dismiss) private var dismiss
    private let title: String
    private let onSaved: (Components.Schemas.Ingredient) -> Void

    init(name: String, repository: IngredientsRepository, onCreated: @escaping (Components.Schemas.Ingredient) -> Void) {
        self.title = "Custom ingredient"
        self.onSaved = onCreated
        _viewModel = State(initialValue: CustomIngredientViewModel(name: name, repository: repository))
    }

    init(editing ingredient: Components.Schemas.Ingredient, repository: IngredientsRepository, onSaved: @escaping (Components.Schemas.Ingredient) -> Void) {
        self.title = "Edit ingredient"
        self.onSaved = onSaved
        _viewModel = State(initialValue: CustomIngredientViewModel(editing: ingredient, repository: repository))
    }

    var body: some View {
        @Bindable var viewModel = viewModel
        NavigationStack {
            Form {
                Section("Ingredient") {
                    field("Name", text: $viewModel.form.name, field: .name, id: "customIngredientNameField", numeric: false)
                    Picker("Category", selection: $viewModel.form.category) {
                        ForEach(Components.Schemas.IngredientCategory.allCases, id: \.self) { Text($0.label).tag($0) }
                    }
                }
                Section("Per 100 g (leave blank if unknown)") {
                    field("Calories (kcal)", text: $viewModel.form.calories, field: .calories, id: "customIngredientCaloriesField")
                    field("Protein (g)", text: $viewModel.form.protein, field: .protein, id: "customIngredientProteinField")
                    field("Carbohydrates (g)", text: $viewModel.form.carbohydrates, field: .carbohydrates, id: "customIngredientCarbsField")
                    field("Fat (g)", text: $viewModel.form.fat, field: .fat, id: "customIngredientFatField")
                }
                Section("Conversions (optional)") {
                    field("Grams per piece", text: $viewModel.form.gramsPerPiece, field: .gramsPerPiece, id: "customIngredientPieceField")
                    field("Density (g/ml)", text: $viewModel.form.density, field: .density, id: "customIngredientDensityField")
                }
                if let banner = viewModel.bannerError {
                    Section { Text(banner).foregroundStyle(.red) }
                }
                Section {
                    Button("Save ingredient") {
                        Task { if let ingredient = await viewModel.save() { onSaved(ingredient) } }
                    }
                    .disabled(viewModel.isSaving)
                    .accessibilityIdentifier("customIngredientSaveButton")
                }
            }
            .scrollDismissesKeyboard(.interactively)
            .navigationTitle(title)
            .inlineNavigationTitle()
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
            }
        }
    }

    @ViewBuilder
    private func field(_ title: String, text: Binding<String>, field: CustomIngredientForm.Field, id: String, numeric: Bool = true) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            if numeric {
                TextField(title, text: text).decimalKeyboard().accessibilityIdentifier(id)
            } else {
                TextField(title, text: text).accessibilityIdentifier(id)
            }
            if let message = viewModel.fieldErrors[field] {
                Text(message).font(.caption).foregroundStyle(.red)
            }
        }
    }
}
