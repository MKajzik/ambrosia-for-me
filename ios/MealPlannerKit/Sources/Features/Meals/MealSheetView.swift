import SwiftUI

/// Loads the meal and shows the editor or the read-only view: the loaded meal's `isOwner` decides, so an
/// editor never appears for a meal the caller does not own.
struct MealSheetView: View {
    @State private var editor: MealEditorViewModel
    private let dependencies: MealsDependencies
    private let mealsViewModel: MealsViewModel
    @Environment(\.dismiss) private var dismiss

    init(mealID: String, dependencies: MealsDependencies, partnerLinked: Bool, mealsViewModel: MealsViewModel) {
        self.dependencies = dependencies
        self.mealsViewModel = mealsViewModel
        _editor = State(initialValue: MealEditorViewModel(mealID: mealID, repository: dependencies.meals, partnerLinked: partnerLinked))
    }

    var body: some View {
        NavigationStack {
            Group {
                switch editor.phase {
                case .loading:
                    ProgressView()
                case .failed(let message):
                    VStack(spacing: 12) {
                        Text(message).multilineTextAlignment(.center)
                        Button("Try again") { Task { await editor.load() } }
                    }
                    .padding()
                case .unavailable(let message):
                    VStack(spacing: 12) {
                        Text(message).multilineTextAlignment(.center)
                        Button("Close") { dismiss() }
                    }
                    .padding()
                case .ready:
                    if editor.isOwner {
                        MealEditorView(editor: editor, mealsViewModel: mealsViewModel, ingredients: dependencies.ingredients)
                    } else {
                        MealReadOnlyView(editor: editor, mealsViewModel: mealsViewModel)
                    }
                }
            }
            .navigationTitle(editor.meal?.name ?? "Meal")
            .inlineNavigationTitle()
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") { dismiss() }.accessibilityIdentifier("mealSheetDoneButton")
                }
            }
        }
        .task { await editor.load() }
    }
}
