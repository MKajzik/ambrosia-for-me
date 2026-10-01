import SwiftUI

public struct SignInView: View {
    @Bindable var viewModel: AuthViewModel
    let onSignedIn: () -> Void
    let onShowRegister: () -> Void

    public init(viewModel: AuthViewModel, onSignedIn: @escaping () -> Void, onShowRegister: @escaping () -> Void) {
        self.viewModel = viewModel
        self.onSignedIn = onSignedIn
        self.onShowRegister = onShowRegister
    }

    public var body: some View {
        Form {
            Section {
                TextField("Email", text: $viewModel.email)
                    .textContentType(.emailAddress)
                    .autocorrectionDisabled()
                    #if os(iOS)
                    .keyboardType(.emailAddress)
                    .textInputAutocapitalization(.never)
                    #endif
                    .accessibilityIdentifier("signInEmailField")
                SecureField("Password", text: $viewModel.password)
                    .textContentType(.password)
                    .accessibilityIdentifier("signInPasswordField")
            }
            if let errorMessage = viewModel.errorMessage {
                Text(errorMessage)
                    .foregroundStyle(.red)
                    .accessibilityIdentifier("signInErrorMessage")
            }
            Button("Sign In") {
                Task {
                    if await viewModel.signIn() != nil {
                        onSignedIn()
                    }
                }
            }
            .disabled(viewModel.isSubmitting || viewModel.email.isEmpty || viewModel.password.isEmpty)
            .accessibilityIdentifier("signInSubmitButton")
            Button("Create an account", action: onShowRegister)
                .accessibilityIdentifier("showRegisterButton")
        }
        .navigationTitle("Sign In")
    }
}
