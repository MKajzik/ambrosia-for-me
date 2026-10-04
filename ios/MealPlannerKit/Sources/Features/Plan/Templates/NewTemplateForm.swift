import SwiftUI

/// Name and day count, both required by `createDietTemplate`. On success the same sheet becomes the editor
/// (`TemplatesViewModel.createTemplate` sets `presentation = .edit(id)`). The day count cannot change afterwards.
struct NewTemplateForm: View {
    let viewModel: TemplatesViewModel
    @State private var name = ""
    @State private var dayCount = "7"
    @State private var isCreating = false
    @State private var error: String?
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        Form {
            Section {
                TextField("Name", text: $name).accessibilityIdentifier("newTemplateNameField")
                TextField("Number of days (1 to 31)", text: $dayCount)
                    .decimalKeyboard()
                    .accessibilityIdentifier("newTemplateDaysField")
            } footer: {
                Text("The number of days can't be changed later.")
            }
            if let error {
                Section { Text(error).foregroundStyle(.red).accessibilityIdentifier("newTemplateError") }
            }
            Section {
                Button("Create template") {
                    Task {
                        isCreating = true
                        error = await viewModel.createTemplate(name: name, dayCount: dayCount)
                        isCreating = false
                    }
                }
                .disabled(isCreating)
                .accessibilityIdentifier("newTemplateCreateButton")
            }
        }
        .navigationTitle("New template")
        .inlineNavigationTitle()
        .toolbar {
            ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
        }
    }
}
