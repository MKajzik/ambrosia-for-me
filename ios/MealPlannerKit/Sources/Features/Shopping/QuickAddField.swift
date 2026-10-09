import SwiftUI

struct QuickAddField: View {
    let onAdd: (String) async -> Bool
    @State private var text = ""

    var body: some View {
        HStack {
            TextField("Add an item", text: $text)
                .submitLabel(.done)
                .onSubmit(submit)
                .accessibilityIdentifier("quickAddField")
            Button("Add", action: submit)
                .disabled(text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                .accessibilityIdentifier("quickAddButton")
        }
    }

    private func submit() {
        let entered = text
        Task { if await onAdd(entered) { text = "" } }
    }
}
