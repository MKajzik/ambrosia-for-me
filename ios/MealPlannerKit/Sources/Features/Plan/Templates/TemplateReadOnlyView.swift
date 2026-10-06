import SwiftUI

/// A partner's shared template: the same days and slots without controls, plus "Copy to my library", which copies the
/// template (and the meals it uses) and opens the copy in the editor. It cannot be applied: copy it first.
struct TemplateReadOnlyView: View {
    let editor: TemplateEditorViewModel
    let templatesViewModel: TemplatesViewModel
    @State private var isCopying = false
    @State private var copyError: String?

    var body: some View {
        if let template = editor.template {
            List {
                ForEach(0..<template.dayCount, id: \.self) { day in
                    Section("Day \(day + 1)") {
                        let slots = template.slots.filter { $0.dayIndex == day }
                        if slots.isEmpty {
                            Text("Nothing planned").foregroundStyle(.secondary)
                        }
                        ForEach(TemplateDraft.slotOrder, id: \.self) { slot in
                            ForEach(slots.filter { $0.slot == slot }, id: \.id) { entry in
                                HStack {
                                    VStack(alignment: .leading, spacing: 2) {
                                        Text(TemplateEditorView.label(slot)).font(.caption).foregroundStyle(.secondary)
                                        Text(entry.mealName)
                                    }
                                    Spacer()
                                    if entry.portion != 1 {
                                        Text("× \(plainNumber(entry.portion))").foregroundStyle(.secondary)
                                    }
                                }
                            }
                        }
                    }
                }
                Section {
                    Button("Copy to my library") {
                        Task {
                            isCopying = true
                            copyError = await templatesViewModel.copyToLibrary(id: template.id)
                            isCopying = false
                        }
                    }
                    .disabled(isCopying)
                    .accessibilityIdentifier("copyTemplateButton")
                }
            }
            .alert(
                "Couldn't copy the template",
                isPresented: Binding(get: { copyError != nil }, set: { if !$0 { copyError = nil } })
            ) {
                Button("OK", role: .cancel) {}
            } message: {
                Text(copyError ?? "")
            }
        }
    }
}
