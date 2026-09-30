public protocol TokenStore: Sendable {
    var accessToken: String? { get }
    var refreshToken: String? { get }
    func save(accessToken: String, refreshToken: String)
    func clear()
}
