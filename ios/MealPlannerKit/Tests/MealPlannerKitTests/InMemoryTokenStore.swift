import Auth
import Foundation

final class InMemoryTokenStore: TokenStore, @unchecked Sendable {
    private let lock = NSLock()
    private var _accessToken: String?
    private var _refreshToken: String?

    init(accessToken: String? = nil, refreshToken: String? = nil) {
        self._accessToken = accessToken
        self._refreshToken = refreshToken
    }

    var accessToken: String? {
        lock.lock(); defer { lock.unlock() }
        return _accessToken
    }

    var refreshToken: String? {
        lock.lock(); defer { lock.unlock() }
        return _refreshToken
    }

    func save(accessToken: String, refreshToken: String) {
        lock.lock(); defer { lock.unlock() }
        _accessToken = accessToken
        _refreshToken = refreshToken
    }

    func clear() {
        lock.lock(); defer { lock.unlock() }
        _accessToken = nil
        _refreshToken = nil
    }
}
