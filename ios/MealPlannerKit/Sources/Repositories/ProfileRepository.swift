import API
import Foundation
import Persistence

/// The signed-in user's profile, cache-first: `cachedUser()` then `refreshUser()`. Writes are online-only: each answer
/// replaces the cached user. `PATCH /me` clears a target with an explicit `null`, which the generated types cannot
/// send, so a cleared target goes out as `NullSentinel.value` and `NullSentinelMiddleware` rewrites it.
public struct ProfileRepository: Sendable {
    private let client: Client
    private let cache: ProfileCache

    public init(client: Client, cache: ProfileCache) {
        self.client = client
        self.cache = cache
    }

    public func cachedUser() async -> Components.Schemas.User? { await cache.user() }

    /// `GET /me`; the answer replaces the cached user. A failure leaves the cache untouched.
    @discardableResult
    public func refreshUser() async throws -> Components.Schemas.User {
        let response = try await unwrapping { try await client.getMe(.init()) }
        switch response {
        case .ok(let ok): return await keep(try ok.body.json)
        case .unauthorized: throw ProfileError.unauthorized
        case .tooManyRequests: throw ProfileError.rateLimited
        case .internalServerError: throw ProfileError.server("The server had a problem loading your profile.")
        case .undocumented(let status, _): throw ProfileError.unexpected(status)
        }
    }

    /// Saves all four targets (`nil` clears one). Returns, and caches, the updated user.
    public func updateTargets(_ update: TargetsUpdate) async throws -> Components.Schemas.User {
        let body = Components.Schemas.UpdateProfileRequest(
            targetKcal: update.kcal ?? NullSentinel.value,
            targetProteinG: update.protein ?? NullSentinel.value,
            targetCarbsG: update.carbs ?? NullSentinel.value,
            targetFatG: update.fat ?? NullSentinel.value
        )
        let response = try await unwrapping { try await client.updateMe(.init(body: .json(body))) }
        switch response {
        case .ok(let ok): return await keep(try ok.body.json)
        case .badRequest(let r): throw ProfileError.validation(r.problem)
        case .unauthorized: throw ProfileError.unauthorized
        case .tooManyRequests: throw ProfileError.rateLimited
        case .internalServerError: throw ProfileError.server("The server had a problem saving your targets.")
        case .undocumented(let status, _): throw ProfileError.unexpected(status)
        }
    }

    /// `DELETE /me`: permanent. Does not touch the cache; the caller signs out, which clears every cache.
    public func deleteAccount() async throws {
        let response = try await unwrapping { try await client.deleteMe(.init()) }
        switch response {
        case .noContent: return
        case .unauthorized: throw ProfileError.unauthorized
        case .tooManyRequests: throw ProfileError.rateLimited
        case .internalServerError: throw ProfileError.server("The server had a problem deleting your account.")
        case .undocumented(let status, _): throw ProfileError.unexpected(status)
        }
    }

    /// Called whenever the session ends, so a second user on this device never sees the first user's profile.
    public func clearCaches() async { await cache.clearAll() }

    private func keep(_ user: Components.Schemas.User) async -> Components.Schemas.User {
        await cache.store(user: user)
        return user
    }
}
