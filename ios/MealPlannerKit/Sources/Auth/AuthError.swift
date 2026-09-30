public enum AuthError: Error, Equatable, Sendable {
    /// No refresh token is stored, or the API rejected it (expired, or already used and its
    /// session family revoked). Callers should treat this as "the user must sign in again."
    case signedOut
    case invalidCredentials
    case emailTaken
    case validationFailed(String)
    case rateLimited
    case server(String)
}
