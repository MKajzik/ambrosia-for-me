import Foundation
import SwiftData

/// Builds the SwiftData container. The cache is rebuildable from the API, so there are no migrations:
/// a store that will not open is deleted and recreated.
public enum CacheStore {
    private static let schema = Schema([CachedMeal.self, CachedMealIngredient.self])

    public static var defaultStoreURL: URL {
        URL.applicationSupportDirectory.appending(path: "MealPlannerCache.store")
    }

    public static func inMemoryContainer() throws -> ModelContainer {
        try ModelContainer(for: schema, configurations: ModelConfiguration(isStoredInMemoryOnly: true))
    }

    public static func persistentContainer(at url: URL = defaultStoreURL) throws -> ModelContainer {
        try FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
        do {
            return try open(url)
        } catch {
            removeStore(at: url)
            return try open(url)
        }
    }

    /// The container for app launch: persistent, and in memory as a last resort, so a broken disk
    /// never crashes the app (the cache is optional; the API is the source of truth).
    public static func launchContainer() -> ModelContainer {
        if let container = try? persistentContainer() { return container }
        do {
            return try inMemoryContainer()
        } catch {
            fatalError("Could not create even an in-memory cache: \(error)")
        }
    }

    /// The macro-generated `MealCache` initialiser's access level is not part of this module's contract,
    /// so other modules build the cache through here.
    public static func makeMealCache(_ container: ModelContainer) -> MealCache {
        MealCache(modelContainer: container)
    }

    private static func open(_ url: URL) throws -> ModelContainer {
        try ModelContainer(for: schema, configurations: ModelConfiguration(url: url))
    }

    private static func removeStore(at url: URL) {
        for suffix in ["", "-shm", "-wal"] {
            try? FileManager.default.removeItem(at: URL(fileURLWithPath: url.path + suffix))
        }
    }
}
