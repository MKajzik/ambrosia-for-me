import SwiftUI

/// A partner's shared meal: the same content without controls, plus "Copy to my library", which copies the meal
/// (and any custom ingredients it uses) and opens the copy in the editor.
struct MealReadOnlyView: View {
    let editor: MealEditorViewModel
    let mealsViewModel: MealsViewModel
    @State private var isCopying = false
    @State private var copyError: String?

    var body: some View {
        if let meal = editor.meal {
            List {
                Section {
                    LabeledContent("Servings", value: plainNumber(meal.servings))
                    if let notes = meal.notes, !notes.isEmpty { Text(notes) }
                }
                Section("Ingredients") {
                    ForEach(meal.ingredients.sorted { $0.position < $1.position }, id: \.id) { line in
                        HStack {
                            Text(line.ingredientName)
                            Spacer()
                            Text("\(plainNumber(line.quantity)) \(line.unit.rawValue)").foregroundStyle(.secondary)
                        }
                    }
                }
                Section { NutritionPanelView(nutrition: meal.nutritionPerServing) }
                Section {
                    Button("Copy to my library") {
                        Task {
                            isCopying = true
                            copyError = await mealsViewModel.copyToLibrary(id: meal.id)
                            isCopying = false
                        }
                    }
                    .disabled(isCopying)
                    .accessibilityIdentifier("copyToLibraryButton")
                }
            }
            .alert(
                "Couldn't copy the meal",
                isPresented: Binding(get: { copyError != nil }, set: { if !$0 { copyError = nil } })
            ) {
                Button("OK", role: .cancel) {}
            } message: {
                Text(copyError ?? "")
            }
        }
    }
}
