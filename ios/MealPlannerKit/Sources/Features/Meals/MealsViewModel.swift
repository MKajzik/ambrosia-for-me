import API
import Foundation
import Observation
import Repositories

/// What the single Meals sheet shows. One enum drives one `.sheet(item:)`: a Mine row opens `.edit`, a
/// partner row `.view`; once the meal loads, its `isOwner` decides which UI appears.
public enum MealPresentation: Identifiable, Equatable, Sendable {
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

    var mealID: String? {
        switch self {
        case .new: nil
        case .edit(let id), .view(let id): id
        }
    }
}

@Observable
@MainActor
public final class MealsViewModel {
    public private(set) var scope: MealScope = .mine
    public private(set) var meals: [Components.Schemas.MealSummary] = []
    public private(set) var isLoading = false
    public private(set) var isStale = false
    public private(set) var loadError: String?
    public private(set) var showsPartnerSegment = false
    public var presentation: MealPresentation?
    public var pendingDelete: Components.Schemas.MealSummary?
    public var alertMessage: String?

    @ObservationIgnored private let mealsRepository: MealsRepository
    @ObservationIgnored private let partnerRepository: PartnerRepository

    public init(meals: MealsRepository, partner: PartnerRepository) {
        self.mealsRepository = meals
        self.partnerRepository = partner
    }

    public static func deleteMessage(name: String) -> String {
        "\"\(name)\" will be removed from your library. This can't be undone."
    }

    /// Tab appears or the scene becomes active: re-read the partnership, then the list.
    public func appear() async {
        await refreshPartnerSegment()
        await load()
    }

    /// Renders the cache at once, then awaits the refresh and re-reads. A failed refresh keeps what is
    /// shown and marks it stale; with nothing cached it shows an error state.
    public func load() async {
        let requested = scope
        isLoading = true
        defer { isLoading = false }
        meals = await mealsRepository.cachedMeals(requested)
        do {
            try await mealsRepository.refreshMeals(requested)
            guard scope == requested else { return }
            meals = await mealsRepository.cachedMeals(requested)
            isStale = false
            loadError = nil
        } catch MealsError.partnerNotLinked {
            showsPartnerSegment = false
            if scope == requested {
                scope = .mine
                await load()
            }
        } catch {
            guard scope == requested else { return }
            isStale = true
            loadError = meals.isEmpty ? ErrorText.message(for: error) : nil
        }
    }

    public func select(_ next: MealScope) async {
        guard next != scope else { return }
        scope = next
        await load()
    }

    /// Validates locally (the API would reject the same things), creates, and turns the sheet into the editor.
    /// Returns the error text, or `nil` on success.
    public func createMeal(name: String, servings: String) async -> String? {
        if let message = MealDraft.nameError(name) { return message }
        guard let value = MealDraft.servingsValue(servings) else { return MealDraft.servingsMessage }
        do {
            let meal = try await mealsRepository.create(name: name.trimmingCharacters(in: .whitespacesAndNewlines), servings: value)
            presentation = .edit(meal.id)
            if scope == .mine { meals = await mealsRepository.cachedMeals(.mine) }
            return nil
        } catch {
            return ErrorText.message(for: error)
        }
    }

    /// The one delete path, shared by the editor and the list swipe. Returns the error text, or `nil` on
    /// success (a meal already gone counts as deleted). Closes the sheet if it was showing this meal.
    public func delete(id: String) async -> String? {
        do {
            try await mealsRepository.delete(id: id)
        } catch MealsError.notFound {
            // Already gone elsewhere: the goal is met.
        } catch {
            return ErrorText.message(for: error)
        }
        if presentation?.mealID == id { presentation = nil }
        meals = await mealsRepository.cachedMeals(scope)
        return nil
    }

    /// The list-swipe confirmation: clears `pendingDelete`, deletes, and reports a refusal through `alertMessage`.
    /// The dialog's button hands over the meal itself: by the time its task runs, SwiftUI has already cleared
    /// `pendingDelete`, so reading it here would find nothing.
    public func confirmDelete(_ meal: Components.Schemas.MealSummary) async {
        pendingDelete = nil
        if let message = await delete(id: meal.id) { alertMessage = message }
    }

    /// Copies a meal (the caller's own or the partner's) into the caller's library and opens the copy.
    /// Returns the error text, or `nil` on success.
    public func copyToLibrary(id: String) async -> String? {
        do {
            let copy = try await mealsRepository.copy(id: id)
            presentation = .edit(copy.id)
            try? await mealsRepository.refreshMeals(.mine)
            if scope == .mine { meals = await mealsRepository.cachedMeals(.mine) }
            return nil
        } catch {
            return ErrorText.message(for: error)
        }
    }

    /// Shown only while `GET /partner` says `active`; a pending invite hides it too. If the request fails
    /// (offline) the segment shows only when partner meals are already cached, since they exist only after a
    /// link was active. Anything but active clears the partner cache.
    private func refreshPartnerSegment() async {
        do {
            let partnership = try await partnerRepository.status()
            showsPartnerSegment = partnership?.status == .active
            if !showsPartnerSegment { await mealsRepository.clearPartnerMeals() }
        } catch {
            showsPartnerSegment = !(await mealsRepository.cachedMeals(.partner)).isEmpty
        }
        if !showsPartnerSegment, scope == .partner { scope = .mine }
    }
}
