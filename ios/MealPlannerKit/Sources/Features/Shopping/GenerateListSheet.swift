import SwiftUI

struct GenerateListSheet: View {
    let day: LocalDay
    let onSubmit: (String, String, String?) async -> ShoppingViewModel.Outcome
    let onOpened: (String) -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var from: Date
    @State private var to: Date
    @State private var name = ""
    @State private var message: String?
    @State private var isSaving = false

    init(
        day: LocalDay, range: (from: String, to: String),
        onSubmit: @escaping (String, String, String?) async -> ShoppingViewModel.Outcome,
        onOpened: @escaping (String) -> Void
    ) {
        self.day = day
        self.onSubmit = onSubmit
        self.onOpened = onOpened
        _from = State(initialValue: day.date(range.from) ?? Date())
        _to = State(initialValue: day.date(range.to) ?? Date())
    }

    var body: some View {
        NavigationStack {
            Form {
                DatePicker("From", selection: $from, displayedComponents: .date)
                DatePicker("To", selection: $to, displayedComponents: .date)
                TextField("List name (optional)", text: $name).accessibilityIdentifier("generateNameField")
                Text("Builds the list from the meals planned on these days.").font(.footnote).foregroundStyle(.secondary)
                if let message { Text(message).foregroundStyle(.red).accessibilityIdentifier("generateMessage") }
            }
            .navigationTitle("Generate list")
            .inlineNavigationTitle()
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Generate") { Task { await generate() } }
                        .disabled(isSaving)
                        .accessibilityIdentifier("generateSubmitButton")
                }
            }
        }
    }

    private func generate() async {
        isSaving = true
        defer { isSaving = false }
        switch await onSubmit(day.day(from: from), day.day(from: to), name) {
        case .opened(let id): onOpened(id)
        case .failed(let text): message = text
        }
    }
}
