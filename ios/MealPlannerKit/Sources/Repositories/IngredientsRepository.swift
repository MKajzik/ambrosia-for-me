import API
import Foundation

/// Not cached: ingredient search is always live.
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

    private static func validation(_ problem: Components.Schemas.Problem?) -> IngredientError {
        guard let problem else { return .validationFailed(fields: [:], message: "The request was not accepted.") }
        return .validationFailed(fields: ProblemText.fieldMessages(problem), message: ProblemText.validation(problem))
    }
}
