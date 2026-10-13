import API
import Foundation
import Observation
import Repositories

/// "My ingredients" on Profile: the custom ingredients, loaded live (every page, filtered on the device because the API
/// has no owner filter) and searched locally. Needs a connection, so offline it is an error with retry, never a stale list.
@Observable
@MainActor
public final class MyIngredientsViewModel {
    public private(set) var all: [Components.Schemas.Ingredient] = []
    public var isLoading: Bool { loadsInFlight > 0 }
    public private(set) var loadError: String?
    public var searchText = ""

    /// `all`, narrowed by the search text (whole-word or not, ignoring case).
    public var shown: [Components.Schemas.Ingredient] {
        let query = searchText.trimmingCharacters(in: .whitespacesAndNewlines)
        return query.isEmpty ? all : all.filter { $0.name.localizedCaseInsensitiveContains(query) }
    }

    /// Loads can overlap (appear, retry), so this counts them.
    private var loadsInFlight = 0
    /// Rows saved and ids deleted while a load is in flight. That load's answer may predate them, so they are applied
    /// on top of it. Forgotten once no load is in flight.
    @ObservationIgnored private var savedDuringLoad: [String: Components.Schemas.Ingredient] = [:]
    @ObservationIgnored private var deletedDuringLoad: Set<String> = []
    @ObservationIgnored private let repository: IngredientsRepository

    public init(repository: IngredientsRepository) {
        self.repository = repository
    }

    public func load() async {
        loadsInFlight += 1
        defer {
            loadsInFlight -= 1
            if loadsInFlight == 0 {
                savedDuringLoad = [:]
                deletedDuringLoad = []
            }
        }
        do {
            let fetched = try await repository.customIngredients()
            let kept = fetched.filter { !deletedDuringLoad.contains($0.id) && savedDuringLoad[$0.id] == nil }
            all = Self.sorted(kept + savedDuringLoad.values)
            loadError = nil
        } catch {
            // A list already on screen stays; with nothing there the error is shown.
            loadError = all.isEmpty ? ErrorText.message(for: error) : nil
        }
    }

    /// A created or edited ingredient: replaces its row (or adds it) and keeps the order.
    public func didSave(_ ingredient: Components.Schemas.Ingredient) {
        if loadsInFlight > 0 {
            savedDuringLoad[ingredient.id] = ingredient
            deletedDuringLoad.remove(ingredient.id)
        }
        var next = all.filter { $0.id != ingredient.id }
        next.append(ingredient)
        all = Self.sorted(next)
    }

    /// Returns the text to show, or `nil` when the ingredient is gone. One already gone elsewhere counts as deleted.
    public func delete(_ ingredient: Components.Schemas.Ingredient) async -> String? {
        do {
            try await repository.delete(id: ingredient.id)
        } catch IngredientError.notFound {
            // Already gone: the goal is met.
        } catch {
            return ErrorText.message(for: error)
        }
        if loadsInFlight > 0 {
            deletedDuringLoad.insert(ingredient.id)
            savedDuringLoad[ingredient.id] = nil
        }
        all.removeAll { $0.id == ingredient.id }
        return nil
    }

    private static func sorted(_ items: [Components.Schemas.Ingredient]) -> [Components.Schemas.Ingredient] {
        items.sorted { $0.name.localizedCaseInsensitiveCompare($1.name) == .orderedAscending }
    }
}
