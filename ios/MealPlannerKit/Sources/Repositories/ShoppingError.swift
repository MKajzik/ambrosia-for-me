import API

public enum ShoppingError: Error, Equatable, Sendable {
    /// The list or item is gone, or access to it was (unshared, unlinked, deleted). Never `403`.
    case notFound
    case partnerNotLinked
    /// `409 version_conflict` on an item edit: `current` is the item as the server has it now.
    case versionConflict(current: Components.Schemas.ShoppingItem)
    case validationFailed(String)
    case unauthorized
    case rateLimited
    case server(String)

    static func unexpected(_ status: Int) -> ShoppingError { .server("Unexpected response (\(status)).") }

    static func validation(_ problem: Components.Schemas.Problem?) -> ShoppingError {
        .validationFailed(problem.map(ProblemText.validation) ?? "The request was not accepted.")
    }
}
