import API
import Repositories
import SwiftUI

/// Choose one of my meals for a slot: a modal sheet with a search field (filtered locally) and a list.
struct MealPickerView: View {
    @State private var viewModel: MealPickerViewModel
    private let title: String
    private let onPick: (Components.Schemas.MealSummary) -> Void
    @Environment(\.dismiss) private var dismiss

    init(meals: MealsRepository, title: String, onPick: @escaping (Components.Schemas.MealSummary) -> Void) {
        self.title = title
        self.onPick = onPick
        _viewModel = State(initialValue: MealPickerViewModel(meals: meals))
    }

    var body: some View {
        @Bindable var viewModel = viewModel
        NavigationStack {
            List {
                Section {
                    TextField("Search my meals", text: $viewModel.query)
                        .accessibilityIdentifier("mealPickerSearchField")
                }
                Section {
                    if viewModel.isLoading && viewModel.meals.isEmpty {
                        ProgressView()
                    }
                    if let message = viewModel.errorMessage {
                        Text(message).foregroundStyle(.red)
                    } else if viewModel.isLibraryEmpty {
                        Text("You have no meals yet. Create one in the Meals tab.").foregroundStyle(.secondary)
                    } else if viewModel.filtered.isEmpty && !viewModel.meals.isEmpty {
                        Text("No meal matches.").foregroundStyle(.secondary)
                    }
                    ForEach(viewModel.filtered, id: \.id) { meal in
                        Button {
                            onPick(meal)
                            dismiss()
                        } label: {
                            HStack {
                                Text(meal.name)
                                Spacer()
                                Text("\(plainNumber(meal.servings)) serving\(meal.servings == 1 ? "" : "s")")
                                    .font(.footnote)
                                    .foregroundStyle(.secondary)
                            }
                        }
                        .foregroundStyle(.primary)
                        .accessibilityIdentifier("mealPickerRow-\(meal.name)")
                    }
                }
            }
            .navigationTitle(title)
            .inlineNavigationTitle()
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
            }
        }
        .task { await viewModel.load() }
    }
}
