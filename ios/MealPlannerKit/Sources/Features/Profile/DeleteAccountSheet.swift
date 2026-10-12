import SwiftUI

/// The one action that cannot be undone. The API asks for no re-authentication, so the sheet asks for the email itself.
struct DeleteAccountSheet: View {
    let email: String
    /// Returns the text to show, or `nil` once the account is gone (the app has signed out and this sheet goes with it).
    let onDelete: (String) async -> String?
    @Environment(\.dismiss) private var dismiss
    @State private var typed = ""
    @State private var message: String?
    @State private var isDeleting = false

    private var matches: Bool { ProfileViewModel.emailMatches(typed, email) }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Text("This permanently deletes your account and everything it owns: your meals, diet templates, plan and shopping lists. A partner keeps only the copies they made.")
                }
                Section {
                    TextField("Type \(email) to confirm", text: $typed)
                        .noAutocapitalization()
                        .accessibilityIdentifier("deleteAccountEmailField")
                    if let message {
                        Text(message).foregroundStyle(.red).accessibilityIdentifier("deleteAccountMessage")
                    }
                }
                Section {
                    Button("Delete my account", role: .destructive) { Task { await delete() } }
                        .disabled(!matches || isDeleting)
                        .accessibilityIdentifier("deleteAccountConfirmButton")
                }
            }
            .navigationTitle("Delete account")
            .inlineNavigationTitle()
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Keep my account") { dismiss() } }
            }
        }
    }

    private func delete() async {
        isDeleting = true
        defer { isDeleting = false }
        message = await onDelete(typed)
    }
}
