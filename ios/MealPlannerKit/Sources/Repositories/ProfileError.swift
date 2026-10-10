import API

public enum ProfileError: Error, Equatable, Sendable {
    /// `fields` maps a server field path (`target_kcal`) to its message so the form can show it inline.
    case validationFailed(fields: [String: String], message: String)
    case unauthorized
    case rateLimited
    case server(String)

    static func unexpected(_ status: Int) -> ProfileError { .server("Unexpected response (\(status)).") }

    static func validation(_ problem: Components.Schemas.Problem?) -> ProfileError {
        guard let problem else { return .validationFailed(fields: [:], message: "The request was not accepted.") }
        return .validationFailed(fields: ProblemText.fieldMessages(problem), message: ProblemText.validation(problem))
    }
}
