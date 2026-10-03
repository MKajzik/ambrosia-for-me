import API
import Repositories
import SwiftUI

/// A modal sheet: search field, category filter, results, tap to add. Empty text browses alphabetically.
/// "Create a custom ingredient" is always the last row (as web), because the catalogue never has everything.
public struct IngredientSearchView: View {
    @State private var viewModel: IngredientSearchViewModel
    @State private var creating = false
    @Environment(\.dismiss) private var dismiss
    private let repository: IngredientsRepository
    private let onPick: (Components.Schemas.Ingredient) -> Void

    public init(repository: IngredientsRepository, onPick: @escaping (Components.Schemas.Ingredient) -> Void) {
        self.repository = repository
        self.onPick = onPick
        _viewModel = State(initialValue: IngredientSearchViewModel(repository: repository))
    }

    private var trimmedText: String { viewModel.text.trimmingCharacters(in: .whitespacesAndNewlines) }

    public var body: some View {
        @Bindable var viewModel = viewModel
        NavigationStack {
            List {
                Section {
                    TextField("Search ingredients", text: $viewModel.text)
                        .accessibilityIdentifier("ingredientSearchField")
                    Picker("Category", selection: $viewModel.category) {
                        Text("All categories").tag(Components.Schemas.IngredientCategory?.none)
                        ForEach(Components.Schemas.IngredientCategory.allCases, id: \.self) { category in
                            Text(category.label).tag(Components.Schemas.IngredientCategory?.some(category))
                        }
                    }
                }
                Section { results }
                Section {
                    Button { creating = true } label: {
                        Label(
                            trimmedText.isEmpty ? "Create a custom ingredient" : "Create a custom ingredient “\(trimmedText)”",
                            systemImage: "plus.circle"
                        )
                    }
                    .accessibilityIdentifier("createCustomIngredientButton")
                }
            }
            .navigationTitle("Add ingredient")
            .inlineNavigationTitle()
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
            }
        }
        .task { viewModel.start() }
        .sheet(isPresented: $creating) {
            CustomIngredientView(name: trimmedText, repository: repository) { ingredient in
                onPick(ingredient)
                creating = false
                dismiss()
            }
        }
    }

    @ViewBuilder
    private var results: some View {
        if viewModel.isSearching {
            Text("Searching…").foregroundStyle(.secondary)
        }
        if let message = viewModel.errorMessage {
            Text(message).foregroundStyle(.red)
        } else if viewModel.results.isEmpty && !viewModel.isSearching {
            Text("No ingredient matches.").foregroundStyle(.secondary)
        }
        ForEach(viewModel.results, id: \.id) { ingredient in
            Button {
                onPick(ingredient)
                dismiss()
            } label: {
                HStack {
                    VStack(alignment: .leading, spacing: 2) {
                        Text(ingredient.name)
                        Text(ingredient.category.label).font(.caption).foregroundStyle(.secondary)
                    }
                    Spacer()
                    if ingredient.isCustom {
                        Text("Custom")
                            .font(.caption2.weight(.semibold))
                            .padding(.horizontal, 6)
                            .padding(.vertical, 2)
                            .background(.quaternary, in: Capsule())
                    }
                }
            }
            .accessibilityIdentifier("ingredientResult-\(ingredient.name)")
        }
    }
}
