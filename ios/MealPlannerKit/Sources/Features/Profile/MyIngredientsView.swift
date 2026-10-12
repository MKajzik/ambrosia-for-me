import API
import Repositories
import SwiftUI

struct MyIngredientsView: View {
    @State private var viewModel: MyIngredientsViewModel
    @State private var creating = false
    @State private var editing: IngredientRef?
    @State private var deleting: IngredientRef?
    @State private var alertMessage: String?
    private let repository: IngredientsRepository

    /// `Components.Schemas.Ingredient` is not `Identifiable`; this is what the sheet and dialog bind to.
    private struct IngredientRef: Identifiable {
        let ingredient: Components.Schemas.Ingredient
        var id: String { ingredient.id }
    }

    init(repository: IngredientsRepository) {
        self.repository = repository
        _viewModel = State(initialValue: MyIngredientsViewModel(repository: repository))
    }

    var body: some View {
        @Bindable var viewModel = viewModel
        List { content }
            .navigationTitle("My ingredients")
            .searchable(text: $viewModel.searchText, prompt: "Search my ingredients")
            .toolbar {
                ToolbarItem(placement: .primaryAction) {
                    Button { creating = true } label: { Label("New ingredient", systemImage: "plus") }
                        .accessibilityIdentifier("myIngredientsNewButton")
                }
            }
            .task { await viewModel.load() }
            .refreshable { await viewModel.load() }
            .sheet(isPresented: $creating) {
                CustomIngredientView(name: viewModel.searchText.trimmingCharacters(in: .whitespacesAndNewlines), repository: repository) { created in
                    creating = false
                    viewModel.didSave(created)
                }
            }
            .sheet(item: $editing) { ref in
                CustomIngredientView(editing: ref.ingredient, repository: repository) { saved in
                    editing = nil
                    viewModel.didSave(saved)
                }
            }
            .confirmationDialog(
                "Delete this ingredient?",
                isPresented: Binding(get: { deleting != nil }, set: { if !$0 { deleting = nil } }),
                titleVisibility: .visible,
                presenting: deleting
            ) { ref in
                Button("Delete ingredient", role: .destructive) { Task { await delete(ref.ingredient) } }
            } message: { ref in
                Text("Delete “\(ref.ingredient.name)”? An ingredient a meal still uses cannot be deleted; remove it from those meals first.")
            }
            .alert(
                "Ingredient",
                isPresented: Binding(get: { alertMessage != nil }, set: { if !$0 { alertMessage = nil } })
            ) {
                Button("OK", role: .cancel) {}
            } message: {
                Text(alertMessage ?? "")
            }
    }

    @ViewBuilder
    private var content: some View {
        if viewModel.isLoading && viewModel.all.isEmpty {
            ProgressView()
        } else if let error = viewModel.loadError, viewModel.all.isEmpty {
            Text(error)
            Button("Try again") { Task { await viewModel.load() } }
        } else if viewModel.shown.isEmpty {
            Text(viewModel.all.isEmpty ? "You haven't created any custom ingredients yet." : "No ingredient matches.")
                .foregroundStyle(.secondary)
        } else {
            ForEach(viewModel.shown, id: \.id) { ingredient in row(ingredient) }
        }
    }

    private func row(_ ingredient: Components.Schemas.Ingredient) -> some View {
        HStack {
            VStack(alignment: .leading) {
                Text(ingredient.name).accessibilityIdentifier("myIngredientRow-\(ingredient.name)")
                Text(ingredient.category.label).font(.footnote).foregroundStyle(.secondary)
            }
            Spacer()
            Menu {
                Button("Edit") { editing = IngredientRef(ingredient: ingredient) }
                    .accessibilityIdentifier("ingredientEditButton")
                Button("Delete", role: .destructive) { deleting = IngredientRef(ingredient: ingredient) }
                    .accessibilityIdentifier("ingredientDeleteButton")
            } label: {
                Image(systemName: "ellipsis.circle")
            }
            .accessibilityLabel("Actions for \(ingredient.name)")
            .accessibilityIdentifier("ingredientMenu-\(ingredient.name)")
        }
    }

    private func delete(_ ingredient: Components.Schemas.Ingredient) async {
        if let message = await viewModel.delete(ingredient) { alertMessage = message }
    }
}
