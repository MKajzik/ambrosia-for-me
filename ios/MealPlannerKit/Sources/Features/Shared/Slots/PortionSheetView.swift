import SwiftUI

/// A small sheet with one decimal field: more than 0 and at most 100. Saving is a `PUT` with the slot's current meal
/// and the new portion (done by the caller through `onSave`, which returns the error text or `nil`).
struct PortionSheetView: View {
    let mealName: String
    let onSave: (Double) async -> String?
    @State private var text: String
    @State private var error: String?
    @State private var isSaving = false
    @Environment(\.dismiss) private var dismiss

    init(mealName: String, initial: Double, onSave: @escaping (Double) async -> String?) {
        self.mealName = mealName
        self.onSave = onSave
        _text = State(initialValue: plainNumber(initial))
    }

    var body: some View {
        NavigationStack {
            Form {
                Section(mealName) {
                    TextField("Portion", text: $text)
                        .decimalKeyboard()
                        .accessibilityIdentifier("portionField")
                    if let error { Text(error).font(.caption).foregroundStyle(.red) }
                }
                Section {
                    Button("Save") {
                        Task {
                            guard let portion = Portion.value(text) else {
                                error = Portion.message
                                return
                            }
                            isSaving = true
                            error = await onSave(portion)
                            isSaving = false
                            if error == nil { dismiss() }
                        }
                    }
                    .disabled(isSaving)
                    .accessibilityIdentifier("portionSaveButton")
                }
            }
            .navigationTitle("Change portion")
            .inlineNavigationTitle()
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
            }
        }
    }
}
