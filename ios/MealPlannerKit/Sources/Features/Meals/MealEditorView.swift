import API
import Repositories
import SwiftUI

/// The owner's editor. No Save button: edits autosave (`MealEditorViewModel`), the status line says where things
/// stand, and leaving the sheet or backgrounding the app flushes a pending edit.
struct MealEditorView: View {
    @Bindable var editor: MealEditorViewModel
    let mealsViewModel: MealsViewModel
    let ingredients: IngredientsRepository
    @State private var showingSearch = false
    @State private var confirmingDelete = false
    @State private var deleteError: String?
    @Environment(\.scenePhase) private var scenePhase

    var body: some View {
        Form {
            Section {
                Text(editor.statusText).accessibilityIdentifier("mealSaveStatus")
                if let banner = editor.bannerMessage {
                    HStack {
                        Text(banner).foregroundStyle(.red)
                        Spacer()
                        Button("Try again") { Task { await editor.retry() } }
                    }
                }
            }
            Section("Meal") {
                TextField("Name", text: $editor.draft.name).accessibilityIdentifier("mealNameField")
                fieldError(editor.validationErrors?.name)
                TextField("Servings", text: $editor.draft.servings).decimalKeyboard().accessibilityIdentifier("mealServingsField")
                fieldError(editor.validationErrors?.servings)
                TextField("Notes", text: $editor.draft.notes, axis: .vertical).lineLimit(1...5)
                fieldError(editor.validationErrors?.notes)
                if editor.offersSharing {
                    Toggle("Share with my partner", isOn: $editor.draft.shared)
                }
            }
            Section("Ingredients") {
                ForEach($editor.draft.rows) { $row in
                    IngredientRowView(row: $row, error: editor.validationErrors?.rows[row.id])
                }
                .onDelete { editor.removeRows(at: $0) }
                fieldError(editor.validationErrors?.ingredients)
                Button("Add ingredient") { showingSearch = true }
                    .disabled(!editor.canAddIngredient)
                    .accessibilityIdentifier("addIngredientButton")
            }
            Section {
                NutritionPanelView(nutrition: editor.meal?.nutritionPerServing ?? .init(), isDimmed: editor.hasUnsaved)
            }
            Section {
                Button("Delete meal", role: .destructive) { confirmingDelete = true }
                    .accessibilityIdentifier("deleteMealButton")
            }
        }
        .scrollDismissesKeyboard(.interactively)
        .onDisappear { editor.flushOnLeave() }
        .onChange(of: scenePhase) { _, phase in
            if phase != .active { editor.flushOnLeave() }
        }
        .sheet(isPresented: $showingSearch) {
            IngredientSearchView(repository: ingredients) { editor.addIngredient($0) }
        }
        .confirmationDialog("Delete this meal?", isPresented: $confirmingDelete, titleVisibility: .visible) {
            Button("Delete meal", role: .destructive) {
                let id = editor.mealID
                Task { deleteError = await mealsViewModel.delete(id: id) }
            }
        } message: {
            Text(MealsViewModel.deleteMessage(name: editor.meal?.name ?? editor.draft.name))
        }
        .alert(
            "Couldn't delete the meal",
            isPresented: Binding(get: { deleteError != nil }, set: { if !$0 { deleteError = nil } })
        ) {
            Button("OK", role: .cancel) {}
        } message: {
            Text(deleteError ?? "")
        }
    }

    @ViewBuilder
    private func fieldError(_ message: String?) -> some View {
        if let message { Text(message).font(.caption).foregroundStyle(.red) }
    }
}

private struct IngredientRowView: View {
    @Binding var row: MealDraft.Row
    let error: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                VStack(alignment: .leading, spacing: 2) {
                    Text(row.name)
                    Text(row.category.label).font(.caption).foregroundStyle(.secondary)
                }
                Spacer()
                TextField("Amount", text: $row.quantity)
                    .decimalKeyboard()
                    .multilineTextAlignment(.trailing)
                    .frame(width: 80)
                    .accessibilityLabel("Amount of \(row.name)")
                Picker("Unit", selection: $row.unit) {
                    ForEach(MealDraft.Unit.allCases, id: \.self) { Text($0.rawValue).tag($0) }
                }
                .labelsHidden()
                .pickerStyle(.menu)
            }
            if let error { Text(error).font(.caption).foregroundStyle(.red) }
        }
    }
}
