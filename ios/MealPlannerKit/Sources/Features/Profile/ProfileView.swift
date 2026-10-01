import SwiftUI
import Auth

public struct ProfileView: View {
    let onSignOut: () -> Void

    public init(onSignOut: @escaping () -> Void) {
        self.onSignOut = onSignOut
    }

    public var body: some View {
        Form {
            Button("Sign Out", role: .destructive, action: onSignOut)
                .accessibilityIdentifier("signOutButton")
        }
        .navigationTitle("Profile")
    }
}
