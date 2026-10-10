import API
import Repositories
import SwiftUI
#if os(iOS)
import UIKit
#endif

struct PartnerSection: View {
    @Bindable var viewModel: PartnerViewModel
    @State private var confirmingUnlink = false

    var body: some View {
        Section {
            switch viewModel.phase {
            case .loading:
                loading
            case .none:
                inviteBlock
                acceptBlock
            case .pending(let expiresAt):
                Text(expiresAt.map { "Waiting for your partner. The code expires \($0.formatted(date: .abbreviated, time: .shortened))." } ?? "Waiting for your partner.")
                    .accessibilityIdentifier("partnerPendingLabel")
                inviteBlock
                Button("Cancel invite", role: .destructive) { Task { await viewModel.unlink() } }
                    .disabled(viewModel.isBusy)
                    .accessibilityIdentifier("partnerCancelInviteButton")
                acceptBlock
            case .linked(let name, let since):
                Text("Linked with \(name)" + (since.map { " since \($0.formatted(date: .abbreviated, time: .omitted))" } ?? ""))
                    .accessibilityIdentifier("partnerLinkedLabel")
                Button("Unlink", role: .destructive) { confirmingUnlink = true }
                    .disabled(viewModel.isBusy)
                    .accessibilityIdentifier("partnerUnlinkButton")
            }
        } header: {
            Text("Partner")
        } footer: {
            Text("Link with one partner to share meals, diet templates and live shopping lists.")
        }
        .confirmationDialog("Unlink from your partner?", isPresented: $confirmingUnlink, titleVisibility: .visible) {
            Button("Unlink", role: .destructive) { Task { await viewModel.unlink() } }
        } message: {
            Text("You will stop seeing each other's shared meals, diet templates and shopping lists. Copies stay with whoever made them.")
        }
        .alert(
            "Partner",
            isPresented: Binding(get: { viewModel.alertMessage != nil }, set: { if !$0 { viewModel.alertMessage = nil } })
        ) {
            Button("OK", role: .cancel) {}
        } message: {
            Text(viewModel.alertMessage ?? "")
        }
    }

    @ViewBuilder
    private var loading: some View {
        if let error = viewModel.loadError {
            Text(error)
            Button("Try again") { Task { await viewModel.appear() } }
        } else {
            ProgressView()
        }
    }

    @ViewBuilder
    private var inviteBlock: some View {
        if let invite = viewModel.invite {
            VStack(alignment: .leading, spacing: 6) {
                Text(InviteCodeFormat.display(invite.code))
                    .font(.system(.title2, design: .monospaced))
                    .textSelection(.enabled)
                    .accessibilityLabel(InviteCodeFormat.spoken(invite.code))
                    .accessibilityIdentifier("partnerInviteCode")
                Text("Shown once. Share it with your partner. It expires \(invite.expiresAt.formatted(date: .abbreviated, time: .shortened)).")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                HStack {
                    ShareLink(item: invite.code) { Label("Share", systemImage: "square.and.arrow.up") }
                        .buttonStyle(.borderless)
                    copyButton(invite.code)
                }
            }
        }
        Button(viewModel.invite == nil ? "Create invite code" : "Create a new code") { Task { await viewModel.createInvite() } }
            .disabled(viewModel.isBusy)
            .accessibilityIdentifier("partnerCreateInviteButton")
    }

    @ViewBuilder
    private func copyButton(_ code: String) -> some View {
        #if os(iOS)
        Button { UIPasteboard.general.string = code } label: { Label("Copy", systemImage: "doc.on.doc") }
            .buttonStyle(.borderless)
        #else
        EmptyView()
        #endif
    }

    @ViewBuilder
    private var acceptBlock: some View {
        VStack(alignment: .leading, spacing: 4) {
            TextField("Enter your partner's code", text: $viewModel.codeText)
                .noAutocapitalization()
                .accessibilityIdentifier("partnerCodeField")
            if let message = viewModel.acceptError {
                Text(message).font(.caption).foregroundStyle(.red).accessibilityIdentifier("partnerAcceptError")
            }
        }
        Button("Link accounts") { Task { await viewModel.accept() } }
            .disabled(viewModel.isBusy)
            .accessibilityIdentifier("partnerLinkButton")
    }
}
