import Foundation
import SwiftData

/// Builds the SwiftData container. Two stores sit behind it. Almost everything is rebuildable from the API and lives in
/// the cache store, which is deleted and recreated if it will not open. `CachedIntent` rows are the user's unsynced
/// shopping changes and are not rebuildable, so they have a store of their own that is never deleted: if it will not
/// open, the launch container falls back to memory and leaves the file for a fix. Keep schema changes
/// lightweight-migratable (`CacheStoreTests.previousSchemaMigrates` is the guard).
public enum CacheStore {
    private static let queueModels: [any PersistentModel.Type] = [CachedIntent.self]
    private static let cacheModels: [any PersistentModel.Type] = [
        CachedMeal.self, CachedMealIngredient.self, CachedPlanDay.self, CachedTargets.self,
        CachedTemplate.self, CachedShoppingSummary.self, CachedShoppingList.self,
        CachedProfileUser.self, CachedPartnershipRow.self,
    ]

    /// The queue keeps the file name the single combined store always had, so queued changes from before the split carry
    /// over (its other tables are dropped by the migration).
    public static var defaultQueueURL: URL {
        URL.applicationSupportDirectory.appending(path: "MealPlannerCache.store")
    }

    public static var defaultCacheURL: URL {
        URL.applicationSupportDirectory.appending(path: "MealPlannerRebuildableCache.store")
    }

    public static func inMemoryContainer() throws -> ModelContainer {
        try ModelContainer(for: Schema(queueModels + cacheModels), configurations: ModelConfiguration(isStoredInMemoryOnly: true))
    }

    public static func persistentContainer(
        queueURL: URL = defaultQueueURL, cacheURL: URL = defaultCacheURL
    ) throws -> ModelContainer {
        for url in [queueURL, cacheURL] {
            try FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
        }
        do {
            return try open(queueURL: queueURL, cacheURL: cacheURL)
        } catch {
            removeStore(at: cacheURL) // never the queue
            return try open(queueURL: queueURL, cacheURL: cacheURL)
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

    public static func makePlanCache(_ container: ModelContainer) -> PlanCache {
        PlanCache(modelContainer: container)
    }

    public static func makeProfileCache(_ container: ModelContainer) -> ProfileCache {
        ProfileCache(modelContainer: container)
    }

    public static func makeTemplateCache(_ container: ModelContainer) -> TemplateCache {
        TemplateCache(modelContainer: container)
    }

    public static func makeShoppingCache(_ container: ModelContainer) -> ShoppingCache {
        ShoppingCache(modelContainer: container)
    }

    private static func open(queueURL: URL, cacheURL: URL) throws -> ModelContainer {
        try ModelContainer(
            for: Schema(queueModels + cacheModels),
            configurations: ModelConfiguration("Queue", schema: Schema(queueModels), url: queueURL),
            ModelConfiguration("Cache", schema: Schema(cacheModels), url: cacheURL)
        )
    }

    private static func removeStore(at url: URL) {
        for suffix in ["", "-shm", "-wal"] {
            try? FileManager.default.removeItem(at: URL(fileURLWithPath: url.path + suffix))
        }
    }
}
