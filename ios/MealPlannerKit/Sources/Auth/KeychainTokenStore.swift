import Foundation
import Security

/// Stores tokens in the Keychain, never in `UserDefaults` or SwiftData (spec §5).
public struct KeychainTokenStore: TokenStore {
    private let service: String

    public init(service: String = "dev.ambrosiaforme.mealplanner.tokens") {
        self.service = service
    }

    public var accessToken: String? { read(account: "access_token") }
    public var refreshToken: String? { read(account: "refresh_token") }

    public func save(accessToken: String, refreshToken: String) {
        write(account: "access_token", value: accessToken)
        write(account: "refresh_token", value: refreshToken)
    }

    public func clear() {
        delete(account: "access_token")
        delete(account: "refresh_token")
    }

    private func read(account: String) -> String? {
        var query = baseQuery(account: account)
        query[kSecReturnData as String] = true
        query[kSecMatchLimit as String] = kSecMatchLimitOne
        var result: AnyObject?
        let status = SecItemCopyMatching(query as CFDictionary, &result)
        guard status == errSecSuccess, let data = result as? Data else { return nil }
        return String(data: data, encoding: .utf8)
    }

    private func write(account: String, value: String) {
        let data = Data(value.utf8)
        let query = baseQuery(account: account)
        let status: OSStatus
        if SecItemCopyMatching(query as CFDictionary, nil) == errSecSuccess {
            status = SecItemUpdate(query as CFDictionary, [kSecValueData as String: data] as CFDictionary)
        } else {
            var addQuery = query
            addQuery[kSecValueData as String] = data
            addQuery[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
            status = SecItemAdd(addQuery as CFDictionary, nil)
        }
        // `save`/`TokenStore` stay non-throwing (every call site treats persisting a token as a
        // fire-and-forget side effect of a successful network response), but a failure here is
        // never silent: it previously was, and a misconfigured build (no keychain entitlement —
        // see `project.yml`'s signing settings) silently broke every authenticated request with
        // no diagnostic trail.
        if status != errSecSuccess {
            print("KeychainTokenStore: failed to save '\(account)' (OSStatus \(status))")
        }
    }

    private func delete(account: String) {
        SecItemDelete(baseQuery(account: account) as CFDictionary)
    }

    private func baseQuery(account: String) -> [String: Any] {
        [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
        ]
    }
}
