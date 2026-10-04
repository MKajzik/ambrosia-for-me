import API
import Foundation
import Observation
import Repositories

/// What the single template sheet shows. A Mine row opens `.edit`, a partner row `.view`; once the template loads,
/// its `isOwner` decides which UI appears.
public enum TemplatePresentation: Identifiable, Equatable, Sendable {
    case new
    case edit(String)
    case view(String)

    public var id: String {
        switch self {
        case .new: "new"
        case .edit(let id): "edit-\(id)"
        case .view(let id): "view-\(id)"
        }
    }

    var templateID: String? {
        switch self {
        case .new: nil
        case .edit(let id), .view(let id): id
        }
    }
}

/// The template library, with the Mine / Partner's rule of the Meals tab: the segment shows only while
/// `GET /partner` says `active`.
@Observable
@MainActor
public final class TemplatesViewModel {
    public private(set) var scope: MealScope = .mine
    public private(set) var templates: [Components.Schemas.DietTemplateSummary] = []
    public private(set) var isLoading = false
    public private(set) var isStale = false
    public private(set) var loadError: String?
    public private(set) var showsPartnerSegment = false
    public var presentation: TemplatePresentation?
    public var pendingDelete: Components.Schemas.DietTemplateSummary?
    public var alertMessage: String?

    @ObservationIgnored private let templatesRepository: TemplatesRepository
    @ObservationIgnored private let partnerRepository: PartnerRepository

    public init(templates: TemplatesRepository, partner: PartnerRepository) {
        self.templatesRepository = templates
        self.partnerRepository = partner
    }

    public static func deleteMessage(name: String) -> String {
        "\"\(name)\" will be removed from your library. This can't be undone."
    }

    /// The screen appears or the scene becomes active: re-read the partnership, then the list.
    public func appear() async {
        await refreshPartnerSegment()
        await load()
    }

    /// Renders the cache at once, then awaits the refresh and re-reads. A failed refresh keeps what is shown and marks
    /// it stale; with nothing cached it shows an error state.
    public func load() async {
        let requested = scope
        isLoading = true
        defer { isLoading = false }
        templates = await templatesRepository.cachedTemplates(requested)
        do {
            try await templatesRepository.refreshTemplates(requested)
            guard scope == requested else { return }
            templates = await templatesRepository.cachedTemplates(requested)
            isStale = false
            loadError = nil
        } catch TemplatesError.partnerNotLinked {
            showsPartnerSegment = false
            if scope == requested {
                scope = .mine
                await load()
            }
        } catch {
            guard scope == requested else { return }
            isStale = true
            loadError = templates.isEmpty ? ErrorText.message(for: error) : nil
        }
    }

    public func select(_ next: MealScope) async {
        guard next != scope else { return }
        scope = next
        await load()
    }

    /// Validates locally (the API would reject the same things), creates, and turns the sheet into the editor.
    /// Returns the error text, or `nil` on success.
    public func createTemplate(name: String, dayCount: String) async -> String? {
        if let message = TemplateDraft.nameError(name) { return message }
        guard let days = Int(dayCount.trimmingCharacters(in: .whitespacesAndNewlines)), (1...31).contains(days) else {
            return "Day count must be between 1 and 31."
        }
        do {
            let template = try await templatesRepository.create(name: name.trimmingCharacters(in: .whitespacesAndNewlines), dayCount: days)
            presentation = .edit(template.id)
            if scope == .mine { templates = await templatesRepository.cachedTemplates(.mine) }
            return nil
        } catch {
            return ErrorText.message(for: error)
        }
    }

    /// The one delete path, shared by the editor and the list swipe. Returns the error text, or `nil` on success (a
    /// template already gone counts as deleted). Closes the sheet if it was showing this template.
    public func delete(id: String) async -> String? {
        do {
            try await templatesRepository.delete(id: id)
        } catch TemplatesError.notFound {
            // Already gone elsewhere: the goal is met.
        } catch {
            return ErrorText.message(for: error)
        }
        if presentation?.templateID == id { presentation = nil }
        templates = await templatesRepository.cachedTemplates(scope)
        return nil
    }

    /// The list-swipe confirmation. The dialog's button hands over the template itself: by the time its task runs,
    /// SwiftUI has already cleared `pendingDelete`.
    public func confirmDelete(_ template: Components.Schemas.DietTemplateSummary) async {
        pendingDelete = nil
        if let message = await delete(id: template.id) { alertMessage = message }
    }

    /// Copies a template (mine or the partner's) into my library and opens the copy. Returns the error text or `nil`.
    public func copyToLibrary(id: String) async -> String? {
        do {
            let copy = try await templatesRepository.copy(id: id)
            presentation = .edit(copy.id)
            try? await templatesRepository.refreshTemplates(.mine)
            if scope == .mine { templates = await templatesRepository.cachedTemplates(.mine) }
            return nil
        } catch {
            return ErrorText.message(for: error)
        }
    }

    private func refreshPartnerSegment() async {
        do {
            let partnership = try await partnerRepository.status()
            showsPartnerSegment = partnership?.status == .active
            if !showsPartnerSegment { await templatesRepository.clearPartnerTemplates() }
        } catch {
            showsPartnerSegment = !(await templatesRepository.cachedTemplates(.partner)).isEmpty
        }
        if !showsPartnerSegment, scope == .partner { scope = .mine }
    }
}
