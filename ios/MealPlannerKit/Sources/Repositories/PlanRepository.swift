import API
import Foundation
import Persistence

public struct PlanSnapshot: Equatable, Sendable {
    public var days: [Components.Schemas.DailyTotal]
    public var targets: Components.Schemas.Targets?
}

/// The plan, cache-first: `cached` then `refresh`. Writes (`setEntry`, `clearSlot`, `apply`) touch no cache, because
/// totals come from `GET /plan`; the caller refreshes the dates a write affected.
public struct PlanRepository: Sendable {
    private let client: Client
    private let cache: PlanCache

    public init(client: Client, cache: PlanCache) {
        self.client = client
        self.cache = cache
    }

    public func cached(from: String, to: String) async -> PlanSnapshot {
        PlanSnapshot(days: await cache.days(from: from, to: to), targets: await cache.targets())
    }

    /// `GET /plan` for `[from, to]`; replaces exactly those dates and the targets. A failure leaves the cache untouched.
    public func refresh(from: String, to: String) async throws {
        let response = try await unwrapping { try await client.getPlan(.init(query: .init(from: from, to: to))) }
        switch response {
        case .ok(let ok):
            let range = try ok.body.json
            await cache.replace(days: range.days, targets: range.targets)
        case .badRequest(let r): throw PlanError.validation(r.problem)
        case .unauthorized: throw PlanError.unauthorized
        case .conflict(let r): throw PlanError.server(r.problem?.detail ?? r.problem?.title ?? "The request conflicts with the current state.")
        case .tooManyRequests: throw PlanError.rateLimited
        case .internalServerError: throw PlanError.server("The server had a problem loading the plan.")
        case .undocumented(let status, _): throw PlanError.unexpected(status)
        }
    }

    /// Sets or swaps the meal for a date and slot (a snack is always added: the API cannot address one among several).
    public func setEntry(date: String, slot: Components.Schemas.Slot, mealID: String, portion: Double) async throws {
        let response = try await unwrapping {
            try await client.setPlanEntry(.init(path: .init(date: date, slot: slot), body: .json(.init(mealId: mealID, portion: portion))))
        }
        switch response {
        case .ok: return
        case .badRequest(let r): throw PlanError.validation(r.problem)
        case .unauthorized: throw PlanError.unauthorized
        case .tooManyRequests: throw PlanError.rateLimited
        case .internalServerError: throw PlanError.server("The server had a problem saving the plan.")
        case .undocumented(let status, _): throw PlanError.unexpected(status)
        }
    }

    /// Removes the entry for a date and slot (for a snack, every snack that day). Clearing a slot that is already
    /// empty (`404`) is a success: the goal is met.
    public func clearSlot(date: String, slot: Components.Schemas.Slot) async throws {
        let response = try await unwrapping { try await client.deletePlanEntry(.init(path: .init(date: date, slot: slot))) }
        switch response {
        case .noContent, .notFound: return
        case .unauthorized: throw PlanError.unauthorized
        case .tooManyRequests: throw PlanError.rateLimited
        case .internalServerError: throw PlanError.server("The server had a problem clearing the slot.")
        case .undocumented(let status, _): throw PlanError.unexpected(status)
        }
    }

    /// Copies a template of mine into the plan from `startDate`. Without `overwrite`, `409 plan_conflict` when a
    /// non-snack slot in the range already has a meal. A partner's template answers `404`.
    public func apply(templateID: String, startDate: String, overwrite: Bool) async throws {
        let response = try await unwrapping {
            try await client.applyDietTemplate(.init(path: .init(id: templateID), body: .json(.init(startDate: startDate, overwrite: overwrite))))
        }
        switch response {
        case .noContent: return
        case .badRequest(let r): throw PlanError.validation(r.problem)
        case .unauthorized: throw PlanError.unauthorized
        case .notFound: throw PlanError.notFound
        case .conflict(let r): throw PlanError.applyConflict(r.problem)
        case .tooManyRequests: throw PlanError.rateLimited
        case .internalServerError: throw PlanError.server("The server had a problem applying the template.")
        case .undocumented(let status, _): throw PlanError.unexpected(status)
        }
    }

    /// Called whenever the session ends, so a second user on this device never sees the first user's plan.
    public func clearCaches() async { await cache.clearAll() }
}
