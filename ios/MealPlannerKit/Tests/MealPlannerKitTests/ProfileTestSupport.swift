import API
import Foundation
import Persistence
@testable import Features
@testable import Repositories

/// A tiny in-memory stand-in for `/me`, so view-model tests read like the real round trip. PATCH follows the real rules:
/// an explicit `null` clears a field, an omitted field is unchanged, an out-of-range number is a `400`.
final class ProfileServer: @unchecked Sendable {
    private let lock = NSLock()
    private var user: Components.Schemas.User
    private var offline = false
    private var forced: [String: Int] = [:]
    private var deleted = false
    private var rejectedField: String?

    init(_ user: Components.Schemas.User = ProfileFixtures.user()) { self.user = user }

    var current: Components.Schemas.User { lock.lock(); defer { lock.unlock() }; return user }
    var isDeleted: Bool { lock.lock(); defer { lock.unlock() }; return deleted }

    func setOffline(_ value: Bool) { lock.lock(); offline = value; lock.unlock() }
    /// Every request for this exact route (`"PATCH /me"`) answers `status`.
    func force(_ route: String, status: Int) { lock.lock(); forced[route] = status; lock.unlock() }
    /// Someone else changed the profile.
    func replace(_ next: Components.Schemas.User) { lock.lock(); user = next; lock.unlock() }
    /// The next PATCH is refused as invalid on this server field (`target_protein_g`), whatever the value.
    func rejectPatch(field: String?) { lock.lock(); rejectedField = field; lock.unlock() }

    /// `GET /me` answers with what the server held when it was asked, but only once this gate opens: a slow read that
    /// a later write overtakes.
    func holdReads(until gate: Gate?) { lock.lock(); readGate = gate; lock.unlock() }

    func route(_ call: RoutingTransport.Call) async throws -> (status: Int, body: String) {
        let answer = try handle(call)
        if call.route == "GET /me", let gate = heldReadGate {
            readsWaiting.mutate { $0 += 1 }
            await gate.wait()
        }
        return answer
    }

    /// How many reads have been held so far, so a test can wait until its slow read is really in flight.
    let readsWaiting = Locked(0)
    private var readGate: Gate?
    private var heldReadGate: Gate? { lock.lock(); defer { lock.unlock() }; return readGate }

    private func handle(_ call: RoutingTransport.Call) throws -> (status: Int, body: String) {
        lock.lock(); defer { lock.unlock() }
        if offline { throw URLError(.notConnectedToInternet) }
        if let status = forced[call.route] { return (status, status >= 400 ? Fixtures.problem(status, code: "forced") : "") }
        switch call.route {
        case "GET /me":
            return (200, Fixtures.json(user))
        case "PATCH /me":
            if let field = rejectedField {
                return (400, Fixtures.problem(400, code: "validation_failed", errors: [(field, "invalid_value")]))
            }
            let body = (try? JSONSerialization.jsonObject(with: Data(call.body.utf8))) as? [String: Any] ?? [:]
            var next = user
            var errors: [(field: String, code: String)] = []
            func read(_ key: String, positive: Bool, max: Double, _ set: (Double?) -> Void) {
                guard let raw = body[key] else { return } // omitted: unchanged
                if raw is NSNull { set(nil); return } // explicit null: clear
                guard let value = (raw as? NSNumber)?.doubleValue, positive ? value > 0 : value >= 0, value <= max else {
                    errors.append((key, "invalid_value"))
                    return
                }
                set(value)
            }
            read("target_kcal", positive: true, max: 20000) { next.targetKcal = $0 }
            read("target_protein_g", positive: false, max: 2000) { next.targetProteinG = $0 }
            read("target_carbs_g", positive: false, max: 5000) { next.targetCarbsG = $0 }
            read("target_fat_g", positive: false, max: 2000) { next.targetFatG = $0 }
            if !errors.isEmpty { return (400, Fixtures.problem(400, code: "validation_failed", errors: errors)) }
            user = next
            return (200, Fixtures.json(next))
        case "DELETE /me":
            deleted = true
            return (204, "")
        default:
            return (500, Fixtures.problem(500, code: "unrouted"))
        }
    }
}

/// A profile repository, plan repository and caches over one `ProfileServer`. The client has the real null-sentinel
/// middleware, so a cleared target travels the same way it does in the app.
@MainActor
struct ProfileHarness {
    let server: ProfileServer
    let transport: RoutingTransport
    let profileCache: ProfileCache
    let planCache: PlanCache
    let profile: ProfileRepository
    let plan: PlanRepository
    let signOuts = Locked(0)

    init(_ server: ProfileServer = ProfileServer()) throws {
        let transport = RoutingTransport { call in try await server.route(call) }
        let container = try CacheStore.inMemoryContainer()
        let client = makeClient(transport: transport, middlewares: [NullSentinelMiddleware()])
        self.server = server
        self.transport = transport
        self.profileCache = CacheStore.makeProfileCache(container)
        self.planCache = CacheStore.makePlanCache(container)
        self.profile = ProfileRepository(client: client, cache: profileCache)
        self.plan = PlanRepository(client: client, cache: planCache)
    }

    func makeViewModel() -> ProfileViewModel {
        let counter = signOuts
        return ProfileViewModel(profile: profile, plan: plan, signOut: { counter.mutate { $0 += 1 } })
    }
}
