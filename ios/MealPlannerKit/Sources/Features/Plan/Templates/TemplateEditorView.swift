import API
import Repositories
import SwiftUI

/// The owner's editor: name, sharing, then one section per day with breakfast, lunch and dinner (one meal each) and
/// any number of snacks. No Save button: edits autosave, the status line says where things stand, and leaving the
/// sheet or backgrounding the app flushes a pending edit.
struct TemplateEditorView: View {
    @Bindable var editor: TemplateEditorViewModel
    let templatesViewModel: TemplatesViewModel
    let meals: MealsRepository
    @State private var picking: SlotPick?
    @State private var confirmingDelete = false
    @State private var deleteError: String?
    @Environment(\.scenePhase) private var scenePhase

    struct SlotPick: Identifiable {
        let dayIndex: Int
        let slot: Components.Schemas.Slot
        var id: String { "\(dayIndex)-\(slot.rawValue)" }
    }

    var body: some View {
        Form {
            Section {
                Text(editor.statusText).accessibilityIdentifier("templateSaveStatus")
                if let banner = editor.bannerMessage {
                    HStack {
                        Text(banner).foregroundStyle(.red)
                        Spacer()
                        Button("Try again") { Task { await editor.retry() } }
                    }
                }
            }
            Section("Template") {
                TextField("Name", text: $editor.draft.name).accessibilityIdentifier("templateNameField")
                if let message = editor.validationErrors?.name { Text(message).font(.caption).foregroundStyle(.red) }
                if editor.offersSharing {
                    Toggle("Share with my partner", isOn: $editor.draft.shared)
                }
                LabeledContent("Days", value: "\(editor.draft.dayCount)")
            }
            ForEach(0..<editor.draft.dayCount, id: \.self) { day in
                Section("Day \(day + 1)") {
                    ForEach([Components.Schemas.Slot.breakfast, .lunch, .dinner], id: \.self) { slot in
                        slotRows(day: day, slot: slot)
                    }
                    snackRows(day: day)
                }
            }
            Section {
                Button("Delete template", role: .destructive) { confirmingDelete = true }
                    .accessibilityIdentifier("deleteTemplateButton")
            }
        }
        .scrollDismissesKeyboard(.interactively)
        .onDisappear { editor.flushOnLeave() }
        .onChange(of: scenePhase) { _, phase in
            if phase != .active { editor.flushOnLeave() }
        }
        .sheet(item: $picking) { pick in
            MealPickerView(meals: meals, title: "\(Self.label(pick.slot)) · Day \(pick.dayIndex + 1)") { meal in
                editor.setMeal(dayIndex: pick.dayIndex, slot: pick.slot, meal: meal)
            }
        }
        .confirmationDialog("Delete this template?", isPresented: $confirmingDelete, titleVisibility: .visible) {
            Button("Delete template", role: .destructive) {
                let id = editor.templateID
                Task { deleteError = await templatesViewModel.delete(id: id) }
            }
        } message: {
            Text(TemplatesViewModel.deleteMessage(name: editor.template?.name ?? editor.draft.name))
        }
        .alert(
            "Couldn't delete the template",
            isPresented: Binding(get: { deleteError != nil }, set: { if !$0 { deleteError = nil } })
        ) {
            Button("OK", role: .cancel) {}
        } message: {
            Text(deleteError ?? "")
        }
    }

    @ViewBuilder
    private func slotRows(day: Int, slot: Components.Schemas.Slot) -> some View {
        let rows = editor.draft.rows(day: day, slot: slot)
        if let row = rows.first {
            slotRow(row, title: Self.label(slot), pick: SlotPick(dayIndex: day, slot: slot))
        } else {
            Button("Add \(Self.label(slot).lowercased())") { picking = SlotPick(dayIndex: day, slot: slot) }
        }
    }

    @ViewBuilder
    private func snackRows(day: Int) -> some View {
        ForEach(editor.draft.rows(day: day, slot: .snack)) { row in
            slotRow(row, title: "Snack", pick: nil)
        }
        Button("Add snack") { picking = SlotPick(dayIndex: day, slot: .snack) }
    }

    /// One slot: the meal (tap to swap, for breakfast, lunch and dinner), its portion, swipe to remove.
    private func slotRow(_ row: TemplateDraft.Row, title: String, pick: SlotPick?) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                if let pick {
                    Button { picking = pick } label: {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(title).font(.caption).foregroundStyle(.secondary)
                            Text(row.mealName)
                        }
                    }
                    .foregroundStyle(.primary)
                    .buttonStyle(.borderless)
                } else {
                    VStack(alignment: .leading, spacing: 2) {
                        Text(title).font(.caption).foregroundStyle(.secondary)
                        Text(row.mealName)
                    }
                }
                Spacer()
                TextField("Portion", text: portionBinding(row.id))
                    .decimalKeyboard()
                    .multilineTextAlignment(.trailing)
                    .frame(width: 64)
                    .accessibilityLabel("Portion of \(row.mealName)")
            }
            if let message = editor.validationErrors?.rows[row.id] {
                Text(message).font(.caption).foregroundStyle(.red)
            }
        }
        .swipeActions(edge: .trailing) {
            Button("Remove", role: .destructive) { editor.removeRow(id: row.id) }
        }
    }

    private func portionBinding(_ id: String) -> Binding<String> {
        Binding(
            get: { editor.draft.rows.first { $0.id == id }?.portion ?? "" },
            set: { text in
                var next = editor.draft
                if let index = next.rows.firstIndex(where: { $0.id == id }) {
                    next.rows[index].portion = text
                    editor.draft = next
                }
            }
        )
    }

    static func label(_ slot: Components.Schemas.Slot) -> String {
        switch slot {
        case .breakfast: "Breakfast"
        case .lunch: "Lunch"
        case .dinner: "Dinner"
        case .snack: "Snack"
        }
    }
}
