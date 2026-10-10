import API
import Repositories
import SwiftUI

public struct ShoppingView: View {
    @State private var viewModel: ShoppingViewModel
    @State private var opened: String?
    @State private var showingNew = false
    @State private var showingGenerate = false
    private let dependencies: ShoppingDependencies
    private let day = LocalDay()
    @Environment(\.scenePhase) private var scenePhase

    public init(dependencies: ShoppingDependencies) {
        self.dependencies = dependencies
        _viewModel = State(initialValue: ShoppingViewModel(shopping: dependencies.shopping, partner: dependencies.partner))
    }

    public var body: some View {
        @Bindable var viewModel = viewModel
        List {
            if viewModel.showsPartnerSegment {
                Picker(
                    "Lists",
                    selection: Binding(get: { viewModel.scope }, set: { next in Task { await viewModel.select(next) } })
                ) {
                    Text("Mine").tag(MealScope.mine)
                    Text("Partner's").tag(MealScope.partner)
                }
                .pickerStyle(.segmented)
                .listRowBackground(Color.clear)
                .accessibilityIdentifier("shoppingScopePicker")
            }
            if viewModel.isStale {
                Label("Offline: showing saved lists", systemImage: "wifi.slash")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            content
        }
        .navigationTitle("Shopping")
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Menu {
                    Button("New list") { showingNew = true }.accessibilityIdentifier("newListButton")
                    Button("Generate from plan") { showingGenerate = true }.accessibilityIdentifier("generateListButton")
                } label: {
                    Label("Add list", systemImage: "plus")
                }
                .accessibilityIdentifier("addListMenu")
            }
        }
        .refreshable { await viewModel.appear() }
        .task { await viewModel.appear() }
        .task { await viewModel.watchSync(dependencies.sync) }
        .onChange(of: scenePhase) { _, phase in
            if phase == .active { Task { await viewModel.appear() } }
        }
        .navigationDestination(item: $opened) { id in
            ShoppingListView(dependencies: dependencies, listID: id).id(id)
        }
        .sheet(isPresented: $showingNew) {
            NewListSheet(onSubmit: { name, shared in await viewModel.createList(name: name, shared: shared) }) { id in
                showingNew = false
                opened = id
            }
        }
        .sheet(isPresented: $showingGenerate) {
            GenerateListSheet(
                day: day, range: viewModel.defaultRange(),
                onSubmit: { from, to, name in await viewModel.generate(from: from, to: to, name: name) }
            ) { id in
                showingGenerate = false
                opened = id
            }
        }
        .alert(
            "Couldn't load more",
            isPresented: Binding(get: { viewModel.alertMessage != nil }, set: { if !$0 { viewModel.alertMessage = nil } })
        ) {
            Button("OK", role: .cancel) {}
        } message: {
            Text(viewModel.alertMessage ?? "")
        }
    }

    @ViewBuilder
    private var content: some View {
        if viewModel.lists.isEmpty {
            if viewModel.isLoading {
                ProgressView()
            } else if let error = viewModel.loadError {
                VStack(alignment: .leading, spacing: 8) {
                    Text(error)
                    Button("Try again") { Task { await viewModel.load() } }
                }
            } else {
                Text(viewModel.scope == .mine
                    ? "No shopping lists yet. Generate one from your plan, or start an empty one."
                    : "Lists your partner shares with you show up here.")
                    .foregroundStyle(.secondary)
            }
        } else {
            ForEach(viewModel.lists, id: \.id) { list in
                Button { opened = list.id } label: { row(list) }
                    .accessibilityIdentifier("shoppingListRow-\(list.name)")
            }
            if viewModel.hasMore {
                Button(viewModel.isLoadingMore ? "Loading…" : "Load more") { Task { await viewModel.loadMore() } }
                    .disabled(viewModel.isLoadingMore)
                    .accessibilityIdentifier("loadMoreListsButton")
            }
        }
    }

    private func row(_ list: Components.Schemas.ShoppingListSummary) -> some View {
        HStack {
            VStack(alignment: .leading) {
                Text(list.name).foregroundStyle(.primary)
                Text(subtitle(list)).font(.footnote).foregroundStyle(.secondary)
            }
            Spacer()
            if viewModel.pendingListIDs.contains(list.id) {
                Label("Syncing changes", systemImage: "arrow.triangle.2.circlepath")
                    .labelStyle(.iconOnly)
                    .foregroundStyle(.secondary)
                    .accessibilityIdentifier("shoppingListSyncing-\(list.name)")
            }
            if viewModel.scope == .partner {
                Text("Shared by your partner").font(.caption).foregroundStyle(.secondary)
            } else if list.sharedWithPartner {
                Text("Shared").font(.caption).foregroundStyle(.secondary)
            }
        }
    }

    private func subtitle(_ list: Components.Schemas.ShoppingListSummary) -> String {
        guard let from = list.sourceFrom, let to = list.sourceTo else { return "Your own list" }
        return "From your plan · \(day.heading(from)) – \(day.heading(to))"
    }
}
