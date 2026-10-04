import SwiftUI

/// The apply sheet: pick one of my templates and a start date. A `409 plan_conflict` becomes the "Replace?" question.
struct ApplyTemplateView: View {
    @State private var viewModel: ApplyTemplateViewModel
    private let localDay: LocalDay
    @Environment(\.dismiss) private var dismiss

    init(dependencies: PlanDependencies, plan: PlanViewModel, startDate: String) {
        localDay = plan.localDay
        _viewModel = State(initialValue: ApplyTemplateViewModel(templates: dependencies.templates, plan: plan, startDate: startDate))
    }

    var body: some View {
        @Bindable var viewModel = viewModel
        NavigationStack {
            Form {
                Section("Template") {
                    if viewModel.templates.isEmpty {
                        Text(viewModel.isLoading ? "Loading…" : "You have no templates yet.").foregroundStyle(.secondary)
                    } else {
                        Picker("Template", selection: $viewModel.selectedID) {
                            ForEach(viewModel.templates, id: \.id) { template in
                                Text("\(template.name) · \(template.dayCount) day\(template.dayCount == 1 ? "" : "s")")
                                    .tag(Optional(template.id))
                            }
                        }
                        .pickerStyle(.inline)
                        .labelsHidden()
                    }
                }
                Section("Start date") {
                    DatePicker(
                        "Start date",
                        selection: Binding(
                            get: { localDay.date(viewModel.startDate) ?? Date() },
                            set: { viewModel.startDate = localDay.day(from: $0) }
                        ),
                        displayedComponents: .date
                    )
                    .accessibilityIdentifier("applyStartDate")
                }
                if let message = viewModel.errorMessage {
                    Section { Text(message).foregroundStyle(.red) }
                }
                Section {
                    Button("Apply") { Task { await viewModel.apply() } }
                        .disabled(viewModel.isApplying || viewModel.selected == nil)
                        .accessibilityIdentifier("applyButton")
                }
            }
            .navigationTitle("Apply template")
            .inlineNavigationTitle()
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
            }
            .alert("Replace the meals already planned?", isPresented: $viewModel.confirmingReplace) {
                Button("Replace", role: .destructive) { Task { await viewModel.confirmReplace() } }
                Button("Cancel", role: .cancel) {}
            } message: {
                Text("Days from \(localDay.heading(viewModel.startDate)) already have meals in these slots.")
            }
            .onChange(of: viewModel.didApply) { _, done in
                if done { dismiss() }
            }
        }
        .task { await viewModel.load() }
    }
}
