import Testing
@testable import Auth

@Suite
struct KeychainTokenStoreTests {
    @Test("Save, read and clear round-trip through the real Keychain")
    func roundTrip() {
        let store = KeychainTokenStore(service: "dev.ambrosiaforme.mealplanner.tokens.tests")
        store.clear()
        #expect(store.accessToken == nil)
        #expect(store.refreshToken == nil)

        store.save(accessToken: "a1", refreshToken: "r1")
        #expect(store.accessToken == "a1")
        #expect(store.refreshToken == "r1")

        store.save(accessToken: "a2", refreshToken: "r2")
        #expect(store.accessToken == "a2")
        #expect(store.refreshToken == "r2")

        store.clear()
        #expect(store.accessToken == nil)
        #expect(store.refreshToken == nil)
    }
}
