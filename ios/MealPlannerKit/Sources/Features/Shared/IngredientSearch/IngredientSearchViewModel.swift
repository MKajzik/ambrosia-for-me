import API
import Foundation
import Observation
import Repositories

/// Type-ahead over the ingredient catalogue: 250 ms debounce on text, immediate on category; previous
/// results stay on screen while the next search runs. Empty text browses alphabetically (first page).
@Observable
@MainActor
public final class IngredientSearchViewModel {
    public typealias Category = Components.Schemas.IngredientCategory

    public private(set) var results: [Components.Schemas.Ingredient] = []
    public private(set) var isSearching = false
    public private(set) var errorMessage: String?

    private var storedText = ""
    private var storedCategory: Category?

    public var text: String {
        get { storedText }
        set {
            storedText = newValue
            scheduleSearch(debounced: true)
        }
    }

    /// `nil` means all categories.
    public var category: Category? {
        get { storedCategory }
        set {
            storedCategory = newValue
            scheduleSearch(debounced: false)
        }
    }

    @ObservationIgnored private var searchTask: Task<Void, Never>?
    @ObservationIgnored private let repository: IngredientsRepository
    @ObservationIgnored private let sleep: @Sendable (Duration) async throws -> Void

    public init(
        repository: IngredientsRepository,
        sleep: @escaping @Sendable (Duration) async throws -> Void = { try await Task.sleep(for: $0) }
    ) {
        self.repository = repository
        self.sleep = sleep
    }

    /// Called when the sheet opens, so it shows the alphabetical first page straight away.
    public func start() {
        scheduleSearch(debounced: false)
    }

    private func scheduleSearch(debounced: Bool) {
        searchTask?.cancel()
        let query = String(storedText.trimmingCharacters(in: .whitespacesAndNewlines).prefix(100))
        let category = storedCategory
        let sleep = self.sleep
        searchTask = Task { [weak self] in
            if debounced {
                do { try await sleep(.milliseconds(250)) } catch { return }
                guard !Task.isCancelled else { return }
            }
            await self?.run(query: query, category: category)
        }
    }

    private func run(query: String, category: Category?) async {
        isSearching = true
        errorMessage = nil
        do {
            let found = try await repository.search(text: query, category: category)
            guard !Task.isCancelled else { return }
            results = found
        } catch {
            guard !Task.isCancelled else { return }
            errorMessage = ErrorText.message(for: error)
        }
        isSearching = false
    }
}
