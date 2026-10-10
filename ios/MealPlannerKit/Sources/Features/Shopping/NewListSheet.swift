import SwiftUI

struct NewListSheet: View {
    let onSubmit: (String, Bool) async -> ShoppingViewModel.Outcome
    let onOpened: (String) -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var name = ""
    @State private var shared = false
    @State private var message: String?
    @State private var isSaving = false

    var body: some View {
        NavigationStack {
            Form {
                TextField("List name", text: $name).accessibilityIdentifier("newListNameField")
                Toggle("Share with partner", isOn: $shared)
                if let message { Text(message).foregroundStyle(.red) }
            }
            .navigationTitle("New list")
            .inlineNavigationTitle()
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Create") { Task { await create() } }
                        .disabled(isSaving)
                        .accessibilityIdentifier("newListSubmitButton")
                }
            }
        }
    }

    private func create() async {
        isSaving = true
        defer { isSaving = false }
        switch await onSubmit(name, shared) {
        case .opened(let id): onOpened(id)
        case .failed(let text): message = text
        }
    }
}
