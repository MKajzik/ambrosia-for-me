import SwiftUI

/// Loads the template and shows the editor or the read-only view: the loaded template's `isOwner` decides, so an
/// editor never appears for a template the caller does not own.
struct TemplateSheetView: View {
    @State private var editor: TemplateEditorViewModel
    private let dependencies: PlanDependencies
    private let templatesViewModel: TemplatesViewModel
    @Environment(\.dismiss) private var dismiss

    init(templateID: String, dependencies: PlanDependencies, partnerLinked: Bool, templatesViewModel: TemplatesViewModel) {
        self.dependencies = dependencies
        self.templatesViewModel = templatesViewModel
        _editor = State(initialValue: TemplateEditorViewModel(templateID: templateID, repository: dependencies.templates, partnerLinked: partnerLinked))
    }

    var body: some View {
        NavigationStack {
            Group {
                switch editor.phase {
                case .loading:
                    ProgressView()
                case .failed(let message):
                    VStack(spacing: 12) {
                        Text(message).multilineTextAlignment(.center)
                        Button("Try again") { Task { await editor.load() } }
                    }
                    .padding()
                case .unavailable(let message):
                    VStack(spacing: 12) {
                        Text(message).multilineTextAlignment(.center)
                        Button("Close") { dismiss() }
                    }
                    .padding()
                case .ready:
                    if editor.isOwner {
                        TemplateEditorView(editor: editor, templatesViewModel: templatesViewModel, meals: dependencies.meals)
                    } else {
                        TemplateReadOnlyView(editor: editor, templatesViewModel: templatesViewModel)
                    }
                }
            }
            .navigationTitle(editor.template?.name ?? "Template")
            .inlineNavigationTitle()
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") { dismiss() }.accessibilityIdentifier("templateSheetDoneButton")
                }
            }
        }
        .task { await editor.load() }
    }
}
