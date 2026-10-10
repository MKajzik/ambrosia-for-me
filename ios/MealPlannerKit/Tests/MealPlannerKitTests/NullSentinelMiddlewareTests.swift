import API
import Foundation
import HTTPTypes
import OpenAPIRuntime
import Testing
@testable import Repositories

@Suite
struct NullSentinelMiddlewareTests {
    private func client(_ transport: RoutingTransport) -> Client {
        makeClient(transport: transport, middlewares: [NullSentinelMiddleware()])
    }

    @Test("updateMe: the sentinel becomes null; real numbers (including 0) stay numbers")
    func updateMe() async throws {
        let transport = RoutingTransport { _ in (200, Fixtures.json(ProfileFixtures.user())) }
        _ = try await client(transport).updateMe(.init(body: .json(.init(
            targetKcal: NullSentinel.value, targetProteinG: 70, targetCarbsG: 250, targetFatG: 0
        ))))
        let body = try #require(await transport.calls("PATCH /me").first).body
        #expect(body.contains("\"target_kcal\":null"))
        #expect(body.contains("\"target_protein_g\":70"))
        #expect(body.contains("\"target_carbs_g\":250"))
        #expect(body.contains("\"target_fat_g\":0"))
    }

    @Test("updateMe with no sentinel is sent unchanged: no nulls appear")
    func noSentinel() async throws {
        let transport = RoutingTransport { _ in (200, Fixtures.json(ProfileFixtures.user())) }
        _ = try await client(transport).updateMe(.init(body: .json(.init(targetKcal: 2000, targetProteinG: 70))))
        let body = try #require(await transport.calls("PATCH /me").first).body
        #expect(body.contains("\"target_kcal\":2000"))
        #expect(!body.contains("null"))
    }

    @Test("updateIngredient: weight per piece clears, density stays; a -1 nested in nutrients is left alone")
    func updateIngredient() async throws {
        let transport = RoutingTransport { _ in (200, Fixtures.json(Fixtures.ingredient(isCustom: true))) }
        _ = try await client(transport).updateIngredient(.init(
            path: .init(id: "ing-1"),
            body: .json(.init(
                name: "Jam", gramsPerPiece: NullSentinel.value, densityGPerMl: 1.2,
                nutrients: .init(calories: NullSentinel.value)
            ))
        ))
        let body = try #require(await transport.calls("PATCH /ingredients/ing-1").first).body
        #expect(body.contains("\"grams_per_piece\":null"))
        #expect(body.contains("\"density_g_per_ml\":1.2"))
        #expect(body.contains("\"calories\":-1"))
    }

    @Test("Other operations are untouched, even with the same value")
    func otherOperations() async throws {
        let transport = RoutingTransport { _ in (201, Fixtures.json(Fixtures.ingredient(isCustom: true))) }
        _ = try await client(transport).createIngredient(.init(body: .json(.init(
            name: "Jam", category: .other, gramsPerPiece: NullSentinel.value
        ))))
        let body = try #require(await transport.calls("POST /ingredients").first).body
        #expect(body.contains("\"grams_per_piece\":-1"))
    }

    @Test("The rewritten body can be sent twice, so the bearer middleware's retry after a 401 still carries it")
    func replayable() async throws {
        let seen = Locked<HTTPBody?>(nil)
        _ = try await NullSentinelMiddleware().intercept(
            HTTPRequest(method: .patch, scheme: nil, authority: nil, path: "/me"),
            body: HTTPBody(#"{"target_kcal":-1}"#), baseURL: URL(string: "http://localhost")!, operationID: "updateMe"
        ) { _, body, _ in
            seen.set(body)
            return (HTTPResponse(status: .ok), nil)
        }
        #expect(seen.value?.iterationBehavior == .multiple)
    }

    @Test("A body that is not a JSON object passes through")
    func notAnObject() async throws {
        let seen = Locked<String>("")
        _ = try await NullSentinelMiddleware().intercept(
            HTTPRequest(method: .patch, scheme: nil, authority: nil, path: "/me"),
            body: HTTPBody("[1,2]"), baseURL: URL(string: "http://localhost")!, operationID: "updateMe"
        ) { _, body, _ in
            if let body { seen.set(try await String(collecting: body, upTo: 1024)) }
            return (HTTPResponse(status: .ok), nil)
        }
        #expect(seen.value == "[1,2]")
    }
}
