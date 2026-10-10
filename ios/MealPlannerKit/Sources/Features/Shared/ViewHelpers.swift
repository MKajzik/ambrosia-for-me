#if os(iOS)
import UIKit
#endif
import SwiftUI

extension View {
    /// `.keyboardType` is UIKit-only; the package also builds for macOS so `swift test` runs without a simulator.
    @ViewBuilder
    func decimalKeyboard() -> some View {
        #if os(iOS)
        self.keyboardType(.decimalPad)
        #else
        self
        #endif
    }

    @ViewBuilder
    func inlineNavigationTitle() -> some View {
        #if os(iOS)
        self.navigationBarTitleDisplayMode(.inline)
        #else
        self
        #endif
    }

    /// `.textInputAutocapitalization` is UIKit-only; the package also builds for macOS so `swift test` runs without a simulator.
    @ViewBuilder
    func noAutocapitalization() -> some View {
        #if os(iOS)
        self.textInputAutocapitalization(.never).autocorrectionDisabled()
        #else
        self
        #endif
    }

    /// Fires when the device's date or time zone changes (midnight, travelling). iOS only; elsewhere it does nothing.
    @ViewBuilder
    func onSignificantTimeChange(perform action: @escaping () -> Void) -> some View {
        #if os(iOS)
        self.onReceive(NotificationCenter.default.publisher(for: UIApplication.significantTimeChangeNotification)) { _ in action() }
        #else
        self
        #endif
    }
}
