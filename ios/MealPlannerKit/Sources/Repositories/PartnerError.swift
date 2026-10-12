import API

public enum PartnerError: Error, Equatable, Sendable {
    /// `404 invite_invalid`: a wrong, expired, own or already used code (the server answers all four the same).
    case inviteInvalid
    /// `409 partner_already_linked`.
    case alreadyLinked
    case validationFailed(String)
    case unauthorized
    case rateLimited
    case server(String)

    static func unexpected(_ status: Int) -> PartnerError { .server("Unexpected response (\(status)).") }

    static func validation(_ problem: Components.Schemas.Problem?) -> PartnerError {
        .validationFailed(problem.map(ProblemText.validation) ?? "The request was not accepted.")
    }

    static func conflict(_ problem: Components.Schemas.Problem?) -> PartnerError {
        if problem?.code == "partner_already_linked" { return .alreadyLinked }
        return .server(problem?.detail ?? problem?.title ?? "The request conflicts with the current state.")
    }
}
