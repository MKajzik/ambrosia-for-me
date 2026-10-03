import API
import Foundation
import Testing
@testable import Repositories

@Suite
struct IngredientsPartnerRepositoryTests {
    @Test("Search with text sends q, the category and limit 20")
    func searchWithText() async throws {
        let transport = RoutingTransport { _ in (200, Fixtures.ingredientList([Fixtures.ingredient(name: "Rice")])) }
        let repo = IngredientsRepository(client: makeAuthlessClient(transport: transport))
        let found = try await repo.search(text: "  rice ", category: .produce)
        #expect(found.map(\.name) == ["Rice"])
        let path = try #require(await transport.calls.first?.path)
        #expect(path.contains("q=rice"))
        #expect(path.contains("category=produce"))
        #expect(path.contains("limit=20"))
    }

    @Test("Search with no text browses: no q is sent")
    func browse() async throws {
        let transport = RoutingTransport { _ in (200, Fixtures.ingredientList([])) }
        let repo = IngredientsRepository(client: makeAuthlessClient(transport: transport))
        _ = try await repo.search(text: "   ", category: nil)
        let path = try #require(await transport.calls.first?.path)
        #expect(!path.contains("q="))
        #expect(!path.contains("category="))
    }

    @Test("create returns the new ingredient and sends only what was entered")
    func create() async throws {
        let transport = RoutingTransport { _ in (201, Fixtures.json(Fixtures.ingredient(id: "new", name: "Jam", isCustom: true))) }
        let repo = IngredientsRepository(client: makeAuthlessClient(transport: transport))
        let made = try await repo.create(.init(name: "Jam", category: .other, nutrients: .init(calories: 250)))
        #expect(made.id == "new")
        let body = try #require(await transport.calls.first?.body)
        #expect(body.contains("\"calories\":250"))
        #expect(!body.contains("protein"))
    }

    @Test("A 400 on create carries the offending field paths and a combined message")
    func createValidation() async throws {
        let transport = RoutingTransport { _ in
            (400, Fixtures.problem(400, code: "validation_failed", errors: [("nutrients.calories", "invalid_value"), ("name", "required")]))
        }
        let repo = IngredientsRepository(client: makeAuthlessClient(transport: transport))
        do {
            _ = try await repo.create(.init(name: "", category: .other))
            Issue.record("expected a validation error")
        } catch let IngredientError.validationFailed(fields, message) {
            #expect(Set(fields.keys) == ["nutrients.calories", "name"])
            #expect(fields["name"] == "Name is required.")
            #expect(message.contains("Name is required."))
        }
    }

    @Test("Partner status: active and pending come back as the partnership, 404 as nil, other failures throw")
    func partnerStatus() async throws {
        let mode = Locked("active")
        let transport = RoutingTransport { _ in
            switch mode.value {
            case "active": return (200, Fixtures.json(Components.Schemas.Partnership(status: .active, displayName: "Sam", linkedAt: Fixtures.date)))
            case "pending": return (200, Fixtures.json(Components.Schemas.Partnership(status: .pending, expiresAt: Fixtures.date)))
            case "none": return (404, Fixtures.problem(404, code: "partner_not_linked"))
            default: return (500, Fixtures.problem(500, code: "internal"))
            }
        }
        let repo = PartnerRepository(client: makeAuthlessClient(transport: transport))
        #expect(try await repo.status()?.status == .active)
        mode.set("pending")
        #expect(try await repo.status()?.status == .pending)
        mode.set("none")
        #expect(try await repo.status() == nil)
        mode.set("boom")
        await #expect(throws: MealsError.server("The server had a problem loading your partnership.")) { try await repo.status() }
    }
}
