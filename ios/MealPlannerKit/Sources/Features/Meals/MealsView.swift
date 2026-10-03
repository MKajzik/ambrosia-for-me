import API
import Repositories
import SwiftUI

public struct MealsView: View {
    @State private var viewModel: MealsViewModel
    private let dependencies: MealsDependencies
    @Environment(\.scenePhase) private var scenePhase

    public init(dependencies: MealsDependencies) {
        self.dependencies = dependencies
        _viewModel = State(initialValue: MealsViewModel(meals: dependencies.meals, partner: dependencies.partner))
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
                .accessibilityIdentifier("mealsScopePicker")
            }
            if viewModel.isStale {
                Label("Offline: showing saved meals", systemImage: "wifi.slash")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            content
        }
        .navigationTitle("Meals")
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Button { viewModel.presentation = .new } label: { Label("New meal", systemImage: "plus") }
                    .accessibilityIdentifier("newMealButton")
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
            "Delete this meal?",
            isPresented: Binding(get: { viewModel.pendingDelete != nil }, set: { if !$0 { viewModel.pendingDelete = nil } }),
            titleVisibility: .visible,
            presenting: viewModel.pendingDelete
        ) { meal in
            Button("Delete meal", role: .destructive) { Task { await viewModel.confirmDelete(meal) } }
        } message: { meal in
            Text(MealsViewModel.deleteMessage(name: meal.name))
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
        } else if viewModel.meals.isEmpty && !viewModel.isLoading {
            emptyState
        } else {
            ForEach(viewModel.meals, id: \.id) { meal in row(meal) }
        }
    }

    @ViewBuilder
    private var emptyState: some View {
        if viewModel.scope == .mine {
            VStack(alignment: .leading, spacing: 8) {
                Text("No meals yet.").font(.headline)
                Button("Create your first meal") { viewModel.presentation = .new }
                    .accessibilityIdentifier("createFirstMealButton")
            }
        } else {
            Text("Meals your partner shares with you show up here, and you can copy them into your library.")
                .foregroundStyle(.secondary)
        }
    }

    private func row(_ meal: Components.Schemas.MealSummary) -> some View {
        Button {
            viewModel.presentation = viewModel.scope == .mine ? .edit(meal.id) : .view(meal.id)
        } label: {
            HStack {
                VStack(alignment: .leading, spacing: 2) {
                    Text(meal.name)
                    Text("\(plainNumber(meal.servings)) serving\(meal.servings == 1 ? "" : "s")")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
                Spacer()
                if viewModel.scope == .mine && meal.sharedWithPartner {
                    Text("Shared")
                        .font(.caption2.weight(.semibold))
                        .padding(.horizontal, 6)
                        .padding(.vertical, 2)
                        .background(.quaternary, in: Capsule())
                }
            }
        }
        .foregroundStyle(.primary)
        .accessibilityIdentifier("mealRow-\(meal.name)")
        .swipeActions(edge: .trailing) {
            if viewModel.scope == .mine {
                Button("Delete", role: .destructive) { viewModel.pendingDelete = meal }
            } else {
                Button("Copy to my library") {
                    Task { viewModel.alertMessage = await viewModel.copyToLibrary(id: meal.id) }
                }
                .tint(.accentColor)
            }
        }
    }

    @ViewBuilder
    private func sheet(for presentation: MealPresentation) -> some View {
        switch presentation {
        case .new:
            NavigationStack { NewMealForm(viewModel: viewModel) }
        case .edit(let id), .view(let id):
            MealSheetView(mealID: id, dependencies: dependencies, partnerLinked: viewModel.showsPartnerSegment, mealsViewModel: viewModel)
        }
    }
}
