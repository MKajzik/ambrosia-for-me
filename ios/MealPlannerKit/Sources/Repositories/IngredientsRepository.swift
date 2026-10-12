import API
import Foundation

/// Not cached: ingredient search and the custom-ingredient screen are always live.
public struct IngredientsRepository: Sendable {
    private let client: Client

    public init(client: Client) { self.client = client }

    /// With text, one request returns the best matches (unpaginated); without, the first alphabetical page.
    public func search(text: String, category: Components.Schemas.IngredientCategory?) async throws -> [Components.Schemas.Ingredient] {
        let query = text.trimmingCharacters(in: .whitespacesAndNewlines)
        let response = try await unwrapping {
            try await client.listIngredients(.init(query: .init(q: query.isEmpty ? nil : query, category: category, limit: 20)))
        }
        switch response {
        case .ok(let ok): return try ok.body.json.items
        case .badRequest(let r): throw Self.validation(r.problem)
        case .unauthorized: throw IngredientError.unauthorized
        case .tooManyRequests: throw IngredientError.rateLimited
        case .internalServerError: throw IngredientError.server("The server had a problem searching ingredients.")
        case .undocumented(let status, _): throw IngredientError.server("Unexpected response (\(status)).")
        }
    }

    public func create(_ request: Components.Schemas.CreateIngredientRequest) async throws -> Components.Schemas.Ingredient {
        let response = try await unwrapping { try await client.createIngredient(.init(body: .json(request))) }
        switch response {
        case .created(let created): return try created.body.json
        case .badRequest(let r): throw Self.validation(r.problem)
        case .unauthorized: throw IngredientError.unauthorized
        case .tooManyRequests: throw IngredientError.rateLimited
        case .internalServerError: throw IngredientError.server("The server had a problem creating the ingredient.")
        case .undocumented(let status, _): throw IngredientError.server("Unexpected response (\(status)).")
        }
    }

    /// Every custom ingredient the caller has made. `GET /ingredients` has no owner filter, so this walks every page
    /// (100 at a time) and keeps the ones with `isCustom`. A failure on any page fails the whole list.
    public func customIngredients() async throws -> [Components.Schemas.Ingredient] {
        var mine: [Components.Schemas.Ingredient] = []
        var cursor: String?
        repeat {
            let page = try await fetchPage(cursor: cursor)
            mine += page.items.filter(\.isCustom)
            cursor = page.nextCursor
        } while cursor != nil
        return mine
    }

    /// Replaces a custom ingredient's fields and its whole nutrient set. `nil` weight per piece or density clears it
    /// (sent as the null sentinel; see `NullSentinelMiddleware`).
    public func update(id: String, _ update: IngredientUpdate) async throws -> Components.Schemas.Ingredient {
        let body = Components.Schemas.UpdateIngredientRequest(
            name: update.name, category: update.category,
            gramsPerPiece: update.gramsPerPiece ?? NullSentinel.value,
            densityGPerMl: update.densityGPerMl ?? NullSentinel.value,
            nutrients: update.nutrients
        )
        let response = try await unwrapping { try await client.updateIngredient(.init(path: .init(id: id), body: .json(body))) }
        switch response {
        case .ok(let ok): return try ok.body.json
        case .badRequest(let r): throw Self.validation(r.problem)
        case .unauthorized: throw IngredientError.unauthorized
        case .notFound: throw IngredientError.notFound
        case .conflict(let r): throw Self.conflict(r.problem)
        case .tooManyRequests: throw IngredientError.rateLimited
        case .internalServerError: throw IngredientError.server("The server had a problem saving the ingredient.")
        case .undocumented(let status, _): throw IngredientError.server("Unexpected response (\(status)).")
        }
    }

    /// Deletes a custom ingredient. A meal that still uses it blocks the delete (`inUse`).
    public func delete(id: String) async throws {
        let response = try await unwrapping { try await client.deleteIngredient(.init(path: .init(id: id))) }
        switch response {
        case .noContent: return
        case .unauthorized: throw IngredientError.unauthorized
        case .notFound: throw IngredientError.notFound
        case .conflict(let r): throw Self.conflict(r.problem)
        case .tooManyRequests: throw IngredientError.rateLimited
        case .internalServerError: throw IngredientError.server("The server had a problem deleting the ingredient.")
        case .undocumented(let status, _): throw IngredientError.server("Unexpected response (\(status)).")
        }
    }

    private func fetchPage(cursor: String?) async throws -> Components.Schemas.IngredientList {
        let response = try await unwrapping { try await client.listIngredients(.init(query: .init(cursor: cursor, limit: 100))) }
        switch response {
        case .ok(let ok): return try ok.body.json
        case .badRequest(let r): throw Self.validation(r.problem)
        case .unauthorized: throw IngredientError.unauthorized
        case .tooManyRequests: throw IngredientError.rateLimited
        case .internalServerError: throw IngredientError.server("The server had a problem loading ingredients.")
        case .undocumented(let status, _): throw IngredientError.server("Unexpected response (\(status)).")
        }
    }

    private static func conflict(_ problem: Components.Schemas.Problem?) -> IngredientError {
        switch problem?.code {
        case "ingredient_in_use": .inUse
        case "unit_not_convertible": .unitInUse
        default: .server(problem?.detail ?? problem?.title ?? "The request conflicts with the current state.")
        }
    }

    private static func validation(_ problem: Components.Schemas.Problem?) -> IngredientError {
        guard let problem else { return .validationFailed(fields: [:], message: "The request was not accepted.") }
        return .validationFailed(fields: ProblemText.fieldMessages(problem), message: ProblemText.validation(problem))
    }
}
