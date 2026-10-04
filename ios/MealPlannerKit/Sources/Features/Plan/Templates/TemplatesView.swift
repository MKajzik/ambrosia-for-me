import API
import Repositories
import SwiftUI

/// The diet-template library, pushed from Plan's toolbar. Mine / Partner's under the Meals rule.
public struct TemplatesView: View {
    @State private var viewModel: TemplatesViewModel
    private let dependencies: PlanDependencies
    @Environment(\.scenePhase) private var scenePhase

    public init(dependencies: PlanDependencies) {
        self.dependencies = dependencies
        _viewModel = State(initialValue: TemplatesViewModel(templates: dependencies.templates, partner: dependencies.partner))
    }

    public var body: some View {
        @Bindable var viewModel = viewModel
        List {
            if viewModel.showsPartnerSegment {
                Picker(
                    "Library",
                    selection: Binding(get: { viewModel.scope }, set: { next in Task { await viewModel.select(next) } })
                ) {
                    Text("Mine").tag(MealScope.mine)
                    Text("Partner's").tag(MealScope.partner)
                }
                .pickerStyle(.segmented)
                .listRowBackground(Color.clear)
                .accessibilityIdentifier("templatesScopePicker")
            }
            if viewModel.isStale {
                Label("Offline: showing saved templates", systemImage: "wifi.slash")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            content
        }
        .navigationTitle("Diet templates")
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Button { viewModel.presentation = .new } label: { Label("New template", systemImage: "plus") }
                    .accessibilityIdentifier("newTemplateButton")
            }
        }
        .refreshable { await viewModel.appear() }
        .task { await viewModel.appear() }
        .onChange(of: scenePhase) { _, phase in
            if phase == .active { Task { await viewModel.appear() } }
        }
        .sheet(item: $viewModel.presentation, onDismiss: { Task { await viewModel.appear() } }) { presentation in
            sheet(for: presentation).id(presentation.id)
        }
        .confirmationDialog(
            "Delete this template?",
            isPresented: Binding(get: { viewModel.pendingDelete != nil }, set: { if !$0 { viewModel.pendingDelete = nil } }),
            titleVisibility: .visible,
            presenting: viewModel.pendingDelete
        ) { template in
            Button("Delete template", role: .destructive) { Task { await viewModel.confirmDelete(template) } }
        } message: { template in
            Text(TemplatesViewModel.deleteMessage(name: template.name))
        }
        .alert(
            "Couldn't complete that",
            isPresented: Binding(get: { viewModel.alertMessage != nil }, set: { if !$0 { viewModel.alertMessage = nil } })
        ) {
            Button("OK", role: .cancel) {}
        } message: {
            Text(viewModel.alertMessage ?? "")
        }
    }

    @ViewBuilder
    private var content: some View {
        if let error = viewModel.loadError {
            VStack(alignment: .leading, spacing: 8) {
                Text(error)
                Button("Retry") { Task { await viewModel.load() } }
            }
        } else if viewModel.templates.isEmpty && !viewModel.isLoading {
            emptyState
        } else {
            ForEach(viewModel.templates, id: \.id) { template in row(template) }
        }
    }

    @ViewBuilder
    private var emptyState: some View {
        if viewModel.scope == .mine {
            VStack(alignment: .leading, spacing: 8) {
                Text("No templates yet.").font(.headline)
                Button("Create your first template") { viewModel.presentation = .new }
            }
        } else {
            Text("Templates your partner shares with you show up here, and you can copy them into your library.")
                .foregroundStyle(.secondary)
        }
    }

    private func row(_ template: Components.Schemas.DietTemplateSummary) -> some View {
        Button {
            viewModel.presentation = viewModel.scope == .mine ? .edit(template.id) : .view(template.id)
        } label: {
            HStack {
                VStack(alignment: .leading, spacing: 2) {
                    Text(template.name)
                    Text("\(template.dayCount) day\(template.dayCount == 1 ? "" : "s")")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
                Spacer()
                if viewModel.scope == .mine && template.sharedWithPartner {
                    Text("Shared")
                        .font(.caption2.weight(.semibold))
                        .padding(.horizontal, 6)
                        .padding(.vertical, 2)
                        .background(.quaternary, in: Capsule())
                }
            }
        }
        .foregroundStyle(.primary)
        .accessibilityIdentifier("templateRow-\(template.name)")
        .swipeActions(edge: .trailing) {
            if viewModel.scope == .mine {
                Button("Delete", role: .destructive) { viewModel.pendingDelete = template }
            } else {
                Button("Copy to my library") {
                    Task { viewModel.alertMessage = await viewModel.copyToLibrary(id: template.id) }
                }
                .tint(.accentColor)
            }
        }
    }

    @ViewBuilder
    private func sheet(for presentation: TemplatePresentation) -> some View {
        switch presentation {
        case .new:
            NavigationStack { NewTemplateForm(viewModel: viewModel) }
        case .edit(let id), .view(let id):
            TemplateSheetView(templateID: id, dependencies: dependencies, partnerLinked: viewModel.showsPartnerSegment, templatesViewModel: viewModel)
        }
    }
}
