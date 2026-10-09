import API
import Repositories
import SwiftUI

extension Components.Schemas.ShoppingItem: Identifiable {}

public struct ShoppingListView: View {
    @State private var viewModel: ShoppingListViewModel
    @State private var editing: Components.Schemas.ShoppingItem?
    @State private var showingRename = false
    @State private var renameText = ""
    @State private var confirmingDelete = false
    @State private var debugOffline = false
    private let dependencies: ShoppingDependencies
    @Environment(\.dismiss) private var dismiss
    @Environment(\.scenePhase) private var scenePhase

    public init(dependencies: ShoppingDependencies, listID: String) {
        self.dependencies = dependencies
        _viewModel = State(initialValue: ShoppingListViewModel(
            listID: listID, shopping: dependencies.shopping, sync: dependencies.sync,
            events: { dependencies.events.events(listID: $0) }, userID: { dependencies.currentUserID() }
        ))
    }

    public var body: some View {
        Group {
            if viewModel.accessLost {
                ContentUnavailableView("This list is no longer available", systemImage: "cart.badge.minus")
                    .accessibilityIdentifier("listUnavailable")
            } else if let list = viewModel.displayed {
                listBody(list)
            } else if viewModel.isLoading {
                ProgressView()
            } else {
                VStack(spacing: 12) {
                    Text(viewModel.loadError ?? "This list isn't loaded.")
                    Button("Try again") { Task { await viewModel.load() } }
                }
            }
        }
        .navigationTitle(viewModel.displayed?.name ?? "List")
        .inlineNavigationTitle()
        .toolbar { toolbarContent }
        .task { await viewModel.run() }
        .onChange(of: scenePhase) { _, phase in
            if phase == .active { Task { await viewModel.load() } }
        }
        .sheet(item: $editing) { item in
            EditItemSheet(
                item: item,
                onSave: { name, quantity, unit, category in
                    await viewModel.edit(item, name: name, quantity: quantity, unit: unit, category: category)
                },
                onConflict: { editing = $0 },
                onClose: { editing = nil }
            )
            .id("\(item.id)-\(item.version)")
        }
        .alert(
            "Shopping",
            isPresented: Binding(get: { viewModel.alertMessage != nil }, set: { if !$0 { viewModel.alertMessage = nil } })
        ) {
            Button("OK", role: .cancel) {}
        } message: {
            Text(viewModel.alertMessage ?? "")
        }
        .alert("Rename list", isPresented: $showingRename) {
            TextField("Name", text: $renameText)
            Button("Save") { Task { await viewModel.rename(renameText) } }
            Button("Cancel", role: .cancel) {}
        }
        .confirmationDialog("Delete this list?", isPresented: $confirmingDelete, titleVisibility: .visible) {
            Button("Delete list", role: .destructive) {
                Task { if await viewModel.delete() { dismiss() } }
            }
        } message: {
            Text("The list is removed for you and your partner. This can't be undone.")
        }
    }

    private func listBody(_ list: Components.Schemas.ShoppingList) -> some View {
        List {
            Section {
                let progress = viewModel.progress
                Text("\(progress.done) of \(progress.total) bought").foregroundStyle(.secondary)
                if viewModel.isStale {
                    Label("Offline: showing the saved list", systemImage: "wifi.slash").font(.footnote).foregroundStyle(.secondary)
                }
            }
            ForEach(viewModel.groups) { group in
                Section(group.category.label) {
                    ForEach(group.items, id: \.id) { item in
                        ShoppingItemRow(
                            item: item,
                            onToggle: { Task { await viewModel.toggle(item) } },
                            onEdit: { editing = item },
                            onRemove: { Task { await viewModel.remove(item) } }
                        )
                    }
                }
            }
            Section { QuickAddField { await viewModel.quickAdd($0) } }
        }
    }

    @ToolbarContentBuilder
    private var toolbarContent: some ToolbarContent {
        ToolbarItemGroup(placement: .primaryAction) {
            if viewModel.isSyncing {
                Label("Syncing changes", systemImage: "arrow.triangle.2.circlepath")
                    .labelStyle(.iconOnly)
                    .accessibilityIdentifier("syncingBadge")
            }
            if let networkSwitch = dependencies.networkSwitch {
                // UI-test builds only (`-uiTesting`): cut the app off from the API, and bring it back.
                Button(debugOffline ? "Go online" : "Go offline") {
                    debugOffline.toggle()
                    networkSwitch.setOffline(debugOffline)
                    if !debugOffline { Task { await dependencies.sync.drain() } }
                }
                .accessibilityIdentifier("debugOfflineToggle")
            }
            if viewModel.isOwner {
                Menu {
                    Button("Rename") {
                        renameText = viewModel.displayed?.name ?? ""
                        showingRename = true
                    }
                    Button(viewModel.displayed?.sharedWithPartner == true ? "Stop sharing" : "Share with partner") {
                        Task { await viewModel.setShared(!(viewModel.displayed?.sharedWithPartner ?? false)) }
                    }
                    if viewModel.displayed?.sourceFrom != nil {
                        Button("Regenerate from plan") { Task { await viewModel.regenerate() } }
                    }
                    Button("Delete", role: .destructive) { confirmingDelete = true }
                } label: {
                    Label("List actions", systemImage: "ellipsis.circle")
                }
                .accessibilityIdentifier("listMenu")
            }
        }
    }
}
