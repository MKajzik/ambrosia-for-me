import API

public enum PlanError: Error, Equatable, Sendable {
    /// `404` on apply: the template does not exist (or is the partner's, which cannot be applied).
    case notFound
    /// `409 plan_conflict` on apply: a non-snack slot in the range already has a meal and `overwrite` was false.
    case conflict
    case validationFailed(String)
    case unauthorized
    case rateLimited
    case server(String)

    static func unexpected(_ status: Int) -> PlanError { .server("Unexpected response (\(status)).") }

    static func validation(_ problem: Components.Schemas.Problem?) -> PlanError {
        .validationFailed(problem.map(ProblemText.validation) ?? "The request was not accepted.")
    }

    /// Only `plan_conflict` is the "ask before replacing" case; any other `409` is just an error.
    static func applyConflict(_ problem: Components.Schemas.Problem?) -> PlanError {
        if problem?.code == "plan_conflict" { return .conflict }
        return .server(problem?.detail ?? problem?.title ?? "The request conflicts with the current state.")
    }
}
