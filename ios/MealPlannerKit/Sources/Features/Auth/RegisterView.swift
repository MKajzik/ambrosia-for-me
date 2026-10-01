import SwiftUI

public struct RegisterView: View {
    @Bindable var viewModel: AuthViewModel
    let onRegistered: () -> Void
    let onShowSignIn: () -> Void

    public init(viewModel: AuthViewModel, onRegistered: @escaping () -> Void, onShowSignIn: @escaping () -> Void) {
        self.viewModel = viewModel
        self.onRegistered = onRegistered
        self.onShowSignIn = onShowSignIn
    }

    public var body: some View {
        Form {
            Section {
                TextField("Display name", text: $viewModel.displayName)
                    .accessibilityIdentifier("registerDisplayNameField")
                TextField("Email", text: $viewModel.email)
                    .textContentType(.emailAddress)
                    .autocorrectionDisabled()
                    #if os(iOS)
                    .keyboardType(.emailAddress)
                    .textInputAutocapitalization(.never)
                    #endif
                    .accessibilityIdentifier("registerEmailField")
                SecureField("Password", text: $viewModel.password)
                    .textContentType(.newPassword)
                    .accessibilityIdentifier("registerPasswordField")
            }
            if let errorMessage = viewModel.errorMessage {
                Text(errorMessage)
                    .foregroundStyle(.red)
                    .accessibilityIdentifier("registerErrorMessage")
            }
            Button("Create Account") {
                Task {
                    if await viewModel.register() != nil {
                        onRegistered()
                    }
                }
            }
            .disabled(
                viewModel.isSubmitting || viewModel.email.isEmpty || viewModel.password.isEmpty
                    || viewModel.displayName.isEmpty
            )
            .accessibilityIdentifier("registerSubmitButton")
            Button("Already have an account? Sign in", action: onShowSignIn)
                .accessibilityIdentifier("showSignInButton")
        }
        .navigationTitle("Create Account")
    }
}
