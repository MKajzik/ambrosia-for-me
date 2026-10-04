import API
import Foundation
import Observation
import Repositories

/// Choose one of my meals for a slot. The list is all of my meals (cache first, then a refresh), filtered locally
/// as web does, because the API's meal list has no search.
@Observable
@MainActor
public final class MealPickerViewModel {
    public var query = ""
    public private(set) var meals: [Components.Schemas.MealSummary] = []
    public private(set) var isLoading = false
    public private(set) var errorMessage: String?

    @ObservationIgnored private let repository: MealsRepository

    public init(meals: MealsRepository) {
        self.repository = meals
    }

    public var filtered: [Components.Schemas.MealSummary] {
        let text = query.trimmingCharacters(in: .whitespacesAndNewlines)
        return text.isEmpty ? meals : meals.filter { $0.name.localizedCaseInsensitiveContains(text) }
    }

    /// Loaded, no error, and genuinely nothing there (so "create a meal first" is true, not a symptom of being offline).
    public var isLibraryEmpty: Bool { meals.isEmpty && !isLoading && errorMessage == nil }

    public func load() async {
        isLoading = true
        defer { isLoading = false }
        meals = await repository.cachedMeals(.mine)
        do {
            try await repository.refreshMeals(.mine)
            meals = await repository.cachedMeals(.mine)
            errorMessage = nil
        } catch {
            errorMessage = meals.isEmpty ? ErrorText.message(for: error) : nil
        }
    }
}
