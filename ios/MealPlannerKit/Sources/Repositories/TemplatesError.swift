import API

public enum TemplatesError: Error, Equatable, Sendable {
    case notFound
    case partnerNotLinked
    case validationFailed(String)
    case unauthorized
    case rateLimited
    case server(String)

    static func unexpected(_ status: Int) -> TemplatesError { .server("Unexpected response (\(status)).") }

    static func validation(_ problem: Components.Schemas.Problem?) -> TemplatesError {
        .validationFailed(problem.map(ProblemText.validation) ?? "The request was not accepted.")
    }

    static func conflict(_ problem: Components.Schemas.Problem?) -> TemplatesError {
        .server(problem?.detail ?? problem?.title ?? "The request conflicts with the current state.")
    }
}
