import API

public enum MealsError: Error, Equatable, Sendable {
    case notFound
    case partnerNotLinked
    /// `409 meal_in_use`: a diet-template slot or plan entry still references the meal.
    case inUse
    case validationFailed(String)
    case unauthorized
    case rateLimited
    case server(String)

    static func unexpected(_ status: Int) -> MealsError { .server("Unexpected response (\(status)).") }

    static func validation(_ problem: Components.Schemas.Problem?) -> MealsError {
        .validationFailed(problem.map(ProblemText.validation) ?? "The request was not accepted.")
    }

    static func conflict(_ problem: Components.Schemas.Problem?) -> MealsError {
        guard let problem else { return .server("The request conflicts with the current state.") }
        return problem.code == "meal_in_use" ? .inUse : .server(problem.detail ?? problem.title)
    }
}

public enum IngredientError: Error, Equatable, Sendable {
    /// `fields` maps a JSON field path (`name`, `nutrients.calories`) to its message so a form can show it inline.
    case validationFailed(fields: [String: String], message: String)
    case unauthorized
    case rateLimited
    case server(String)
}

/// Turns a `400 validation_failed` problem's per-field `errors` into text, as `AuthRepository.validationMessage`
/// does for the auth fields. Unmapped codes read as "<Field> is invalid."; without `errors`, the problem's own text.
enum ProblemText {
    static func validation(_ problem: Components.Schemas.Problem) -> String {
        guard let errors = problem.errors, !errors.isEmpty else { return problem.detail ?? problem.title }
        return errors.map { message(field: $0.field, code: $0.code) }.joined(separator: " ")
    }

    static func fieldMessages(_ problem: Components.Schemas.Problem) -> [String: String] {
        let pairs = (problem.errors ?? []).map { ($0.field, message(field: $0.field, code: $0.code)) }
        return Dictionary(pairs, uniquingKeysWith: { first, _ in first })
    }

    static func message(field: String, code: String) -> String {
        let label = readable(field)
        switch code {
        case "required": return "\(label) is required."
        case "too_long": return "\(label) is too long."
        case "too_short": return "\(label) is too short."
        default: return "\(label) is invalid."
        }
    }

    /// `items.0.quantity` → "Items quantity"; `grams_per_piece` → "Grams per piece".
    private static func readable(_ field: String) -> String {
        let words = field.split(whereSeparator: { $0 == "." || $0 == "_" }).filter { Int($0) == nil }.map(String.init)
        guard let first = words.first else { return "Field" }
        return ([first.capitalized] + words.dropFirst()).joined(separator: " ")
    }
}

extension Components.Responses.BadRequest {
    var problem: Components.Schemas.Problem? { try? body.applicationProblemJson }
}

extension Components.Responses.Conflict {
    var problem: Components.Schemas.Problem? { try? body.applicationProblemJson }
}
