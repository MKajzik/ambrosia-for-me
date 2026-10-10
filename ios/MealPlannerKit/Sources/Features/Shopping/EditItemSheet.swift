import API
import SwiftUI

struct EditItemSheet: View {
    let item: Components.Schemas.ShoppingItem
    let onSave: (String, String, Components.Schemas.Unit?, Components.Schemas.IngredientCategory) async -> ShoppingListViewModel.EditOutcome
    let onConflict: (Components.Schemas.ShoppingItem) -> Void
    let onClose: () -> Void

    @State private var name: String
    @State private var quantity: String
    @State private var unit: Components.Schemas.Unit?
    @State private var category: Components.Schemas.IngredientCategory
    @State private var message: String?
    @State private var isSaving = false

    init(
        item: Components.Schemas.ShoppingItem,
        onSave: @escaping (String, String, Components.Schemas.Unit?, Components.Schemas.IngredientCategory) async -> ShoppingListViewModel.EditOutcome,
        onConflict: @escaping (Components.Schemas.ShoppingItem) -> Void,
        onClose: @escaping () -> Void
    ) {
        self.item = item
        self.onSave = onSave
        self.onConflict = onConflict
        self.onClose = onClose
        _name = State(initialValue: item.name)
        _quantity = State(initialValue: item.quantity.map { $0.formatted(.number.precision(.fractionLength(0...2)).grouping(.never)) } ?? "")
        _unit = State(initialValue: item.unit.flatMap { Components.Schemas.Unit(rawValue: $0.rawValue) })
        _category = State(initialValue: item.category)
    }

    var body: some View {
        NavigationStack {
            Form {
                TextField("Name", text: $name).accessibilityIdentifier("editItemNameField")
                TextField("Quantity", text: $quantity).decimalKeyboard().accessibilityIdentifier("editItemQuantityField")
                Picker("Unit", selection: $unit) {
                    Text("None").tag(Components.Schemas.Unit?.none)
                    ForEach(Components.Schemas.Unit.allCases, id: \.self) { Text($0.rawValue).tag(Components.Schemas.Unit?.some($0)) }
                }
                Picker("Aisle", selection: $category) {
                    ForEach(Components.Schemas.IngredientCategory.allCases, id: \.self) { Text($0.label).tag($0) }
                }
                if let message { Text(message).foregroundStyle(.red).accessibilityIdentifier("editItemMessage") }
            }
            .navigationTitle("Edit item")
            .inlineNavigationTitle()
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel", action: onClose) }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") { Task { await save() } }
                        .disabled(isSaving)
                        .accessibilityIdentifier("editItemSaveButton")
                }
            }
        }
    }

    private func save() async {
        isSaving = true
        defer { isSaving = false }
        switch await onSave(name, quantity, unit, category) {
        case .saved: onClose()
        case .conflict(let current):
            // Reopen on the server's version, so the next save is against what is really there.
            onConflict(current)
        case .failed(let text): message = text
        }
    }
}
