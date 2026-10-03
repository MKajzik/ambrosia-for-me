import API
import Foundation
import Observation
import Repositories

/// Loads one meal and, for the owner, edits it with autosave (the shared `Autosaver`: saves once the valid draft has
/// stopped changing for 700 ms and differs from what the server holds; fields (`PATCH`) then ingredients (`PUT`),
/// each step committed on its own).
@Observable
@MainActor
public final class MealEditorViewModel {
    public enum Phase: Equatable, Sendable {
        case loading
        case ready
        case unavailable(String)
        case failed(String)
    }

    public let mealID: String
    public let partnerLinked: Bool
    public private(set) var phase: Phase = .loading
    /// The server's latest answer. Feeds the nutrition panel; the draft is copied from it only once.
    public private(set) var meal: Components.Schemas.Meal?

    private var storedDraft = MealDraft()
    /// Set through here so every edit, from a binding or a method, schedules autosave. Reads and writes go
    /// through `storedDraft`, which is observed, so views update.
    public var draft: MealDraft {
        get { storedDraft }
        set {
            storedDraft = newValue
            autosaver.schedule()
        }
    }

    /// What the server holds, in the shape validation produces. `nil` until an owned meal has loaded.
    private var saved: MealDraft.Valid?

    @ObservationIgnored private var adoptedDraft: MealDraft?
    @ObservationIgnored private let repository: MealsRepository
    @ObservationIgnored private let sleep: @Sendable (Duration) async throws -> Void
    @ObservationIgnored private lazy var autosaver = Autosaver<MealDraft.Valid>(
        sleep: sleep,
        pending: { [weak self] in self?.pendingValue },
        perform: { [weak self] value in try await self?.save(value) }
    )

    public init(
        mealID: String,
        repository: MealsRepository,
        partnerLinked: Bool,
        sleep: @escaping @Sendable (Duration) async throws -> Void = { try await Task.sleep(for: $0) }
    ) {
        self.mealID = mealID
        self.repository = repository
        self.partnerLinked = partnerLinked
        self.sleep = sleep
    }

    // MARK: Derived state

    public var isOwner: Bool { meal?.isOwner ?? false }
    public var offersSharing: Bool { partnerLinked || draft.shared }
    public var canAddIngredient: Bool { draft.canAddRow }
    public var isSaving: Bool { autosaver.isSaving }
    public var saveError: String? { autosaver.saveError }

    public var validationErrors: MealDraft.Errors? {
        if case .invalid(let errors) = draft.validate() { errors } else { nil }
    }

    /// The valid draft, when it differs from what the server holds.
    private var pendingValue: MealDraft.Valid? {
        guard let saved, case .valid(let value) = draft.validate(), value != saved else { return nil }
        return value
    }

    public var hasUnsaved: Bool { saved != nil && (validationErrors != nil || pendingValue != nil) }

    public var statusText: String {
        if isSaving { return "Saving…" }
        if validationErrors != nil { return "Fix the highlighted fields to save." }
        if pendingValue != nil { return "Unsaved changes" }
        return "All changes saved"
    }

    /// The failure banner shows only while something is still unsaved.
    public var bannerMessage: String? { hasUnsaved ? saveError : nil }

    // MARK: Loading

    public func load() async {
        if phase == .loading, let cached = await repository.cachedMeal(id: mealID) { adopt(cached) }
        do {
            let fresh = try await repository.refreshMeal(id: mealID)
            // While nothing was typed, take the server's meal (a stale cached copy must never become the base
            // of a later write). Once the person has typed, a refresh only updates the nutrition panel.
            if phase != .ready || draft == adoptedDraft { adopt(fresh) } else { meal = fresh }
        } catch MealsError.notFound {
            phase = .unavailable(ErrorText.message(for: MealsError.notFound))
        } catch {
            if phase != .ready { phase = .failed(ErrorText.message(for: error)) }
        }
    }

    private func adopt(_ loaded: Components.Schemas.Meal) {
        meal = loaded
        phase = .ready
        guard loaded.isOwner else { return }
        let adopted = MealDraft(meal: loaded)
        storedDraft = adopted
        adoptedDraft = adopted
        saved = MealDraft.saved(from: loaded)
        autosaver.forget()
    }

    // MARK: Editing

    public func addIngredient(_ ingredient: Components.Schemas.Ingredient) {
        var next = draft
        next.addRow(for: ingredient)
        draft = next
    }

    public func removeRows(at offsets: IndexSet) {
        var next = draft
        next.rows = next.rows.enumerated().filter { !offsets.contains($0.offset) }.map(\.element)
        draft = next
    }

    // MARK: Autosave

    /// The two writes, each committed on its own: a failure in the second leaves the first counted as saved.
    private func save(_ value: MealDraft.Valid) async throws {
        guard var current = saved else { return }
        let changes = MealDraft.changes(from: current, to: value)
        if let patch = changes.patch {
            meal = try await repository.update(id: mealID, patch)
            current = MealDraft.Valid(name: value.name, notes: value.notes, servings: value.servings, shared: value.shared, items: current.items)
            saved = current
        }
        if let items = changes.items {
            meal = try await repository.replaceIngredients(id: mealID, items)
            current.items = value.items
            saved = current
        }
    }

    /// "Try again": forget that the value was tried, and save now.
    public func retry() async {
        await autosaver.retry()
    }

    /// Leaving the editor saves a pending edit in an unstructured task, so dismissing the view does not cancel it.
    public func flushOnLeave() {
        autosaver.flush()
    }
}
