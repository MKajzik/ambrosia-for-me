import API
import Foundation
import Observation
import Repositories

/// Loads one template and, for the owner, edits it with autosave on the shared `Autosaver` (the template counterpart
/// of `MealEditorViewModel`): name and sharing (`PATCH`) then the whole slot list (`PUT`), each step committed on
/// its own. A partner's template loads read-only.
@Observable
@MainActor
public final class TemplateEditorViewModel {
    public enum Phase: Equatable, Sendable {
        case loading
        case ready
        case unavailable(String)
        case failed(String)
    }

    public let templateID: String
    public let partnerLinked: Bool
    public private(set) var phase: Phase = .loading
    /// The server's latest answer; the draft is copied from it only once.
    public private(set) var template: Components.Schemas.DietTemplate?

    private var storedDraft = TemplateDraft()
    /// Set through here so every edit schedules autosave.
    public var draft: TemplateDraft {
        get { storedDraft }
        set {
            storedDraft = newValue
            autosaver.schedule()
        }
    }

    /// What the server holds, in the shape validation produces. `nil` until an owned template has loaded.
    private var saved: TemplateDraft.Valid?

    @ObservationIgnored private var adoptedDraft: TemplateDraft?
    @ObservationIgnored private let repository: TemplatesRepository
    @ObservationIgnored private let sleep: @Sendable (Duration) async throws -> Void
    @ObservationIgnored private lazy var autosaver = Autosaver<TemplateDraft.Valid>(
        sleep: sleep,
        pending: { [weak self] in self?.pendingValue },
        perform: { [weak self] value in try await self?.save(value) }
    )

    public init(
        templateID: String,
        repository: TemplatesRepository,
        partnerLinked: Bool,
        sleep: @escaping @Sendable (Duration) async throws -> Void = { try await Task.sleep(for: $0) }
    ) {
        self.templateID = templateID
        self.repository = repository
        self.partnerLinked = partnerLinked
        self.sleep = sleep
    }

    // MARK: Derived state

    public var isOwner: Bool { template?.isOwner ?? false }
    public var offersSharing: Bool { partnerLinked || draft.shared }
    public var isSaving: Bool { autosaver.isSaving }
    public var saveError: String? { autosaver.saveError }

    public var validationErrors: TemplateDraft.Errors? {
        if case .invalid(let errors) = draft.validate() { errors } else { nil }
    }

    private var pendingValue: TemplateDraft.Valid? {
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
        if phase == .loading, let cached = await repository.cachedTemplate(id: templateID) { adopt(cached) }
        do {
            let fresh = try await repository.refreshTemplate(id: templateID)
            // While nothing was typed, take the server's copy; once the person has typed, a refresh only updates
            // what the screen shows from the server (never the draft).
            if phase != .ready || draft == adoptedDraft { adopt(fresh) } else { template = fresh }
        } catch TemplatesError.notFound {
            phase = .unavailable(ErrorText.message(for: TemplatesError.notFound))
        } catch {
            if phase != .ready { phase = .failed(ErrorText.message(for: error)) }
        }
    }

    private func adopt(_ loaded: Components.Schemas.DietTemplate) {
        template = loaded
        phase = .ready
        guard loaded.isOwner else { return }
        let adopted = TemplateDraft(template: loaded)
        storedDraft = adopted
        adoptedDraft = adopted
        saved = TemplateDraft.saved(from: loaded)
        autosaver.forget()
    }

    // MARK: Editing

    public func setMeal(dayIndex: Int, slot: Components.Schemas.Slot, meal: Components.Schemas.MealSummary) {
        var next = draft
        next.setMeal(dayIndex: dayIndex, slot: slot, mealID: meal.id, mealName: meal.name)
        draft = next
    }

    public func removeRow(id: String) {
        var next = draft
        next.removeRow(id: id)
        draft = next
    }

    // MARK: Autosave

    /// The two writes, each committed on its own: a failure in the second leaves the first counted as saved.
    private func save(_ value: TemplateDraft.Valid) async throws {
        guard var current = saved else { return }
        let changes = TemplateDraft.changes(from: current, to: value)
        if let patch = changes.patch {
            template = try await repository.update(id: templateID, patch)
            current = TemplateDraft.Valid(name: value.name, shared: value.shared, slots: current.slots)
            saved = current
        }
        if let slots = changes.slots {
            template = try await repository.replaceSlots(id: templateID, slots)
            current.slots = value.slots
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
