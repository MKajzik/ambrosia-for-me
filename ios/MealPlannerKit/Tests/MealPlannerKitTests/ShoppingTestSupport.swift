import API
import Foundation
import Persistence
@testable import Repositories

/// A repository, a sync engine and a cache over one in-memory `ShoppingServer`, for the view-model suites.
@MainActor
struct ShoppingHarness {
    let server: ShoppingServer
    let transport: RoutingTransport
    let cache: ShoppingCache
    let repository: ShoppingListsRepository
    let engine: ShoppingSyncEngine
    let partner: PartnerRepository

    init(_ server: ShoppingServer = ShoppingServer(), partnerActive: Bool = true) throws {
        let transport = RoutingTransport { call in
            if call.route == "GET /partner" {
                return partnerActive
                    ? (200, Fixtures.json(Components.Schemas.Partnership(status: .active, displayName: "Sam", linkedAt: Fixtures.date)))
                    : (404, Fixtures.problem(404, code: "partner_not_linked"))
            }
            return try await server.route(call)
        }
        let client = makeAuthlessClient(transport: transport)
        let cache = CacheStore.makeShoppingCache(try CacheStore.inMemoryContainer())
        self.server = server
        self.transport = transport
        self.cache = cache
        self.repository = ShoppingListsRepository(client: client, cache: cache)
        self.engine = ShoppingSyncEngine(client: client, cache: cache)
        self.partner = PartnerRepository(client: client)
    }
}
