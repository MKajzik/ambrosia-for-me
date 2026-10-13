import SwiftUI

/// Marks an ingredient the person created, next to its name.
struct CustomBadge: View {
    var body: some View {
        Text("Custom")
            .font(.caption2.weight(.semibold))
            .padding(.horizontal, 6)
            .padding(.vertical, 2)
            .background(.quaternary, in: Capsule())
    }
}
