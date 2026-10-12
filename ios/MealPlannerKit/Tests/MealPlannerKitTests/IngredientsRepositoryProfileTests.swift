import API
import Foundation
import Testing
@testable import Repositories

@Suite
struct IngredientsRepositoryProfileTests {
    private func make(withSentinel: Bool = false, _ route: @escaping RoutingTransport.Route) -> (IngredientsRepository, RoutingTransport) {
        let transport = RoutingTransport(route)
        let client = withSentinel
            ? makeClient(transport: transport, middlewares: [NullSentinelMiddleware()])
            : makeAuthlessClient(transport: transport)
        return (IngredientsRepository(client: client), transport)
    }

    private let change = IngredientUpdate(
        name: "Apple, raw", category: .produce, gramsPerPiece: nil, densityGPerMl: 1.2, nutrients: ProfileFixtures.fullInput()
    )

    @Test("customIngredients walks every page at 100 and keeps only the custom ones")
    func listsMine() async throws {
        let (repo, transport) = make { call in
            if call.path.contains("cursor=c2") {
                return (200, ProfileFixtures.ingredientPage([ProfileFixtures.customIngredient(id: "c", name: "Cherry jam")]))
            }
            return (200, ProfileFixtures.ingredientPage(
                [ProfileFixtures.customIngredient(id: "a", name: "Apple"), Fixtures.ingredient(id: "g1", name: "Rice", isCustom: false)],
                next: "c2"
            ))
        }
        let mine = try await repo.customIngredients()
        #expect(mine.map(\.id) == ["a", "c"])
        let paths = await transport.calls("GET /ingredients").map(\.path)
        #expect(paths.count == 2)
        #expect(paths.allSatisfy { $0.contains("limit=100") })
    }

    @Test("A failed page fails the whole list")
    func listFailure() async throws {
        let (repo, _) = make { _ in (500, Fixtures.problem(500, code: "internal")) }
        await #expect(throws: IngredientError.server("The server had a problem loading ingredients.")) { try await repo.customIngredients() }
    }

    @Test("update sends the whole change; a cleared weight per piece is the sentinel when no middleware runs")
    func updateBody() async throws {
        let (repo, transport) = make { _ in (200, Fixtures.json(ProfileFixtures.customIngredient(name: "Apple, raw"))) }
        let saved = try await repo.update(id: "ing-1", change)
        #expect(saved.name == "Apple, raw")
        let body = try #require(await transport.calls("PATCH /ingredients/ing-1").first).body
        #expect(body.contains("\"name\":\"Apple, raw\""))
        #expect(body.contains("\"category\":\"produce\""))
        #expect(body.contains("\"grams_per_piece\":-1"))
        #expect(body.contains("\"density_g_per_ml\":1.2"))
        // The encoder writes 4.6 as 4.5999999999999996 (the same double), so compare the parsed number, not the text.
        let sent = try #require(try JSONSerialization.jsonObject(with: Data(body.utf8)) as? [String: Any])
        let nutrients = try #require(sent["nutrients"] as? [String: Double])
        #expect(nutrients["vitamin_c"] == 4.6)
        #expect(nutrients["folate"] == 3)
    }

    @Test("Review focus 2: through the middleware a cleared weight per piece is null on the wire and the other nutrients stay")
    func updateClearsThroughMiddleware() async throws {
        let (repo, transport) = make(withSentinel: true) { _ in (200, Fixtures.json(ProfileFixtures.customIngredient())) }
        _ = try await repo.update(id: "ing-1", change)
        let body = try #require(await transport.calls("PATCH /ingredients/ing-1").first).body
        #expect(body.contains("\"grams_per_piece\":null"))
        #expect(body.contains("\"sodium\":1"))
        #expect(body.contains("\"vitamin_b12\":0"))
    }

    @Test("update errors: unit in use, other conflict, validation by field, not found")
    func updateErrors() async throws {
        let mode = Locked("unit")
        let (repo, _) = make { _ in
            switch mode.value {
            case "unit": return (409, Fixtures.problem(409, code: "unit_not_convertible"))
            case "other": return (409, Fixtures.problem(409, code: "something", title: "Conflict"))
            case "invalid": return (400, Fixtures.problem(400, code: "validation_failed", errors: [("grams_per_piece", "invalid_value")]))
            default: return (404, Fixtures.problem(404, code: "not_found"))
            }
        }
        await #expect(throws: IngredientError.unitInUse) { try await repo.update(id: "ing-1", change) }
        mode.set("other")
        await #expect(throws: IngredientError.server("Conflict")) { try await repo.update(id: "ing-1", change) }
        mode.set("invalid")
        await #expect(throws: IngredientError.validationFailed(fields: ["grams_per_piece": "Grams per piece is invalid."], message: "Grams per piece is invalid.")) {
            try await repo.update(id: "ing-1", change)
        }
        mode.set("gone")
        await #expect(throws: IngredientError.notFound) { try await repo.update(id: "ing-1", change) }
    }

    @Test("delete succeeds on 204; a meal still using it is inUse; gone is notFound; others throw")
    func delete() async throws {
        let mode = Locked(204)
        let (repo, transport) = make { _ in
            switch mode.value {
            case 204: return (204, "")
            case 409: return (409, Fixtures.problem(409, code: "ingredient_in_use"))
            case 404: return (404, Fixtures.problem(404, code: "not_found"))
            default: return (429, Fixtures.problem(429, code: "too_many_requests"))
            }
        }
        try await repo.delete(id: "ing-1")
        #expect(await transport.calls("DELETE /ingredients/ing-1").count == 1)
        mode.set(409)
        await #expect(throws: IngredientError.inUse) { try await repo.delete(id: "ing-1") }
        mode.set(404)
        await #expect(throws: IngredientError.notFound) { try await repo.delete(id: "ing-1") }
        mode.set(429)
        await #expect(throws: IngredientError.rateLimited) { try await repo.delete(id: "ing-1") }
    }
}
