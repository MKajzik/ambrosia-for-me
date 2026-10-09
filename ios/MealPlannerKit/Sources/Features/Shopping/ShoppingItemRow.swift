import API
import Persistence
import SwiftUI

struct ShoppingItemRow: View {
    let item: Components.Schemas.ShoppingItem
    let onToggle: () -> Void
    let onEdit: () -> Void
    let onRemove: () -> Void

    /// An item that exists only on this device: it syncs soon; until then it can be removed but not edited.
    private var isPending: Bool { item.id.hasPrefix(ShoppingIntent.tempPrefix) }

    var body: some View {
        HStack(spacing: 12) {
            Button(action: onToggle) {
                HStack(spacing: 12) {
                    Image(systemName: item.checked ? "checkmark.circle.fill" : "circle")
                        .foregroundStyle(item.checked ? Color.accentColor : Color.secondary)
                    VStack(alignment: .leading) {
                        Text(item.name).strikethrough(item.checked).foregroundStyle(item.checked ? .secondary : .primary)
                        let quantity = ItemGrouping.quantityText(item)
                        if !quantity.isEmpty { Text(quantity).font(.footnote).foregroundStyle(.secondary) }
                    }
                    Spacer()
                    if isPending { ProgressView().controlSize(.small) }
                }
            }
            .buttonStyle(.plain)
            .accessibilityIdentifier("shoppingItem-\(item.name)")
            // "checked" would match "unchecked" in a UI-test predicate, so the words differ.
            .accessibilityValue(item.checked ? "bought" : "to buy")

            Menu {
                Button("Edit", action: onEdit).disabled(isPending)
                Button("Remove", role: .destructive, action: onRemove)
            } label: {
                Image(systemName: "ellipsis.circle")
            }
            .accessibilityLabel("More actions for \(item.name)")
        }
        .contextMenu {
            Button("Edit", action: onEdit).disabled(isPending)
            Button("Remove", role: .destructive, action: onRemove)
        }
    }
}
