import API
import Repositories
import SwiftUI

public struct ProfileView: View {
    @State private var viewModel: ProfileViewModel
    @State private var partnerViewModel: PartnerViewModel
    @State private var showingDelete = false
    private let dependencies: ProfileDependencies
    private let onSignOut: () -> Void
    @Environment(\.scenePhase) private var scenePhase

    public init(dependencies: ProfileDependencies, onSignOut: @escaping () -> Void) {
        self.dependencies = dependencies
        self.onSignOut = onSignOut
        _viewModel = State(initialValue: ProfileViewModel(profile: dependencies.profile, plan: dependencies.plan, signOut: { onSignOut() }))
        _partnerViewModel = State(initialValue: PartnerViewModel(partner: dependencies.partner))
    }

    public var body: some View {
        Form {
            if viewModel.isStale {
                Label("Offline: showing your saved profile", systemImage: "wifi.slash")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                    .accessibilityIdentifier("profileOfflineLabel")
            }
            accountSection
            TargetsSection(viewModel: viewModel)
            PartnerSection(viewModel: partnerViewModel)
            Section {
                NavigationLink("My ingredients") { MyIngredientsView(repository: dependencies.ingredients) }
                    .accessibilityIdentifier("myIngredientsRow")
            }
            Section {
                Button("Sign Out", role: .destructive, action: onSignOut)
                    .accessibilityIdentifier("signOutButton")
                Button("Delete account", role: .destructive) { showingDelete = true }
                    .disabled(viewModel.user == nil)
                    .accessibilityIdentifier("deleteAccountButton")
            }
        }
        .scrollDismissesKeyboard(.interactively)
        .keyboardDoneToolbar()
        .navigationTitle("Profile")
        .task { await load() }
        .refreshable { await load() }
        .onChange(of: scenePhase) { _, phase in
            if phase == .active { Task { await load() } }
        }
        .sheet(isPresented: $showingDelete) {
            DeleteAccountSheet(email: viewModel.user?.email ?? "") { typed in
                await viewModel.deleteAccount(typedEmail: typed)
            }
        }
    }

    @ViewBuilder
    private var accountSection: some View {
        Section("Account") {
            if let user = viewModel.user {
                LabeledContent("Email", value: user.email).accessibilityIdentifier("profileEmail")
                LabeledContent("Name", value: user.displayName)
            } else if let error = viewModel.loadError {
                Text(error)
                Button("Try again") { Task { await load() } }
            } else {
                ProgressView()
            }
        }
    }

    private func load() async {
        await viewModel.appear()
        await partnerViewModel.appear()
    }
}
