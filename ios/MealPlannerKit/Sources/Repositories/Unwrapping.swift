import OpenAPIRuntime

/// The generated client wraps every transport failure in `ClientError`. Rethrow the cause so callers
/// see a plain `URLError` and can say "can't reach the server" without importing the networking runtime.
func unwrapping<T>(_ body: () async throws -> T) async throws -> T {
    do {
        return try await body()
    } catch let error as ClientError {
        throw error.underlyingError
    }
}
