# iOS Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stand up the iOS app's project skeleton — an XcodeGen-generated Xcode project wrapping a local Swift package, a generated OpenAPI client, Keychain-backed auth with transparent token refresh, email/password sign-in and registration, a five-tab placeholder shell, and CI — so later plans (Meals, Plan and Today, Shopping and Profile, Sign in with Apple) have a working, tested foundation to build on.

**Architecture:** `ios/project.yml` (XcodeGen) generates a thin `MealPlanner` app target plus a `MealPlannerUITests` target; almost all code lives in the local Swift package `ios/MealPlannerKit`, split into four targets — `API` (Swift OpenAPI Generator output + a thin client wrapper), `Auth` (Keychain token storage, single-flight refresh, the bearer middleware, register/login/logout), `Features` (SwiftUI views and view models, one per product area), and `AppCore` (the `@Observable` app state and the root view that switches between sign-in and the tab shell). `swift build`/`swift test` work on the package alone, without Xcode, for fast local and CI unit-test iteration; `xcodebuild` is only needed for the app target and XCUITest.

**Tech Stack:** Swift 6.2 (Swift 6 language mode), SwiftUI, XcodeGen, Swift OpenAPI Generator + OpenAPIRuntime + OpenAPIURLSession, Swift Testing (unit tests), XCTest/XCUITest (UI tests), GitHub Actions on a macOS runner.

**Spec:** `docs/superpowers/specs/2026-09-30-ios-app-design.md` (§3 project structure and networking decisions, §4 project layout, §5 networking and auth, §9 app structure, §13 delivery/CI). Also read `docs/superpowers/specs/2026-09-21-meal-planner-design.md` §2.5 and §4.1 (auth endpoints) and `openapi.yaml` `/auth/*` and `/me` paths, which this plan's generated client targets directly.

## Global Constraints

- iOS 26+ only (parent spec, locked decisions). The local package also declares macOS 15+ so `swift test` runs natively on the build machine without booting a simulator (spec §4: "`swift build`/`swift test` work... without Xcode at all").
- Swift 6 language mode, strict concurrency (parent spec §2.5).
- `openapi.yaml` at the repo root is the only source of truth for the API contract; the generated client is checked into git and drift-checked (`make check-generated-ios`), never hand-edited (root `CLAUDE.md` "Never hand-edit generated code").
- Tokens live only in the Keychain, never `UserDefaults` or SwiftData (spec §5).
- No SwiftData, offline queue or SSE in this plan — those belong to the Meals and Shopping plans (spec §15). Keep this plan's `Features` screens as literal placeholders.
- No Sign in with Apple in this plan (spec §15, item 5) — email/password only.
- This session runs natively on macOS 26.1 with Xcode 26.3 and Swift 6.2.4 already installed; XcodeGen is not yet installed (spec §2). Every step below runs directly in this session.

## Review Focus

- **A second registration with an email already in use.** The API answers `409` with code `email_taken` (`backend/CLAUDE.md`); the register screen must show a specific, distinct error, not a generic failure message — pinned in Task 4's `AuthRepositoryTests` and Task 5's `AuthViewModelTests`.
- **Wrong password or unknown email on sign-in.** The API answers `401` identically for both (`backend/CLAUDE.md` "Login answers unknown email and wrong password identically"); the app must show one generic "invalid email or password" message, never imply which one was wrong — pinned in Task 4.
- **Two requests racing an expired access token.** Both must trigger only one `/auth/refresh` call, and both must retry successfully with the new token — pinned in Task 3's `TokenRefresherTests` (this is the single most important behavior in this plan: get it wrong and every signed-in user is logged out under normal concurrent use, exactly like the bug the web app's BFF was built to avoid).
- **A refresh token that the API rejects** (expired, or already used and the whole family revoked). The refresher must clear the Keychain and surface a "signed out" state rather than retrying forever or leaving stale tokens behind — pinned in Task 3.
- **A validation error from the register/login schema** (`400` with `code: validation_failed` and an `errors` array, e.g. a password under 10 characters). The UI must show that specific message, not a blank or generic one — pinned in Task 4.

---

## Task 1: Xcode project skeleton and Swift package scaffold

**Files:**
- Create: `ios/project.yml`
- Create: `ios/MealPlanner/MealPlannerApp.swift`
- Create: `ios/MealPlanner/Assets.xcassets/Contents.json`
- Create: `ios/MealPlanner/Assets.xcassets/AppIcon.appiconset/Contents.json`
- Create: `ios/MealPlanner/Assets.xcassets/AccentColor.colorset/Contents.json`
- Create: `ios/MealPlannerUITests/SmokeUITests.swift`
- Create: `ios/MealPlannerKit/Package.swift`
- Create: `ios/MealPlannerKit/Sources/AppCore/Placeholder.swift`
- Modify: `.gitignore` (repo root)

**Interfaces:**
- Produces: the `MealPlannerKit` local package with one library target, `AppCore` (placeholder only in this task — real content arrives in Task 5), that the `MealPlanner` app target links against as `import AppCore`. Later tasks add `API`, `Auth` and `Features` targets to the same `Package.swift`.

- [ ] **Step 1: Install XcodeGen**

```bash
brew install xcodegen
xcodegen --version
```

Expected: prints a version number (XcodeGen is not yet installed in this environment, confirmed during brainstorming).

- [ ] **Step 2: Create the local Swift package with a placeholder target**

Create `ios/MealPlannerKit/Package.swift`:

```swift
// swift-tools-version:6.2
import PackageDescription

let package = Package(
    name: "MealPlannerKit",
    // iOS 26+ per the parent spec's locked decisions. macOS is also declared, only so
    // `swift test` can run natively on the build machine without a simulator (spec §4).
    platforms: [.iOS(.v26), .macOS(.v15)],
    products: [
        .library(name: "MealPlannerKit", targets: ["AppCore"])
    ],
    targets: [
        .target(name: "AppCore")
    ]
)
```

Create `ios/MealPlannerKit/Sources/AppCore/Placeholder.swift`:

```swift
/// Replaced by real app state and the root view in Task 5.
enum FoundationPlaceholder {}
```

- [ ] **Step 3: Verify the package builds**

Run: `cd ios/MealPlannerKit && swift build`
Expected: `Build complete!` with no errors.

- [ ] **Step 4: Write the XcodeGen project spec**

Create `ios/project.yml`:

```yaml
name: MealPlanner
options:
  bundleIdPrefix: dev.ambrosiaforme
  deploymentTarget:
    iOS: "26.0"
  createIntermediateGroups: true
packages:
  MealPlannerKit:
    path: MealPlannerKit
targets:
  MealPlanner:
    type: application
    platform: iOS
    sources:
      - MealPlanner
    dependencies:
      - package: MealPlannerKit
    info:
      path: Generated/MealPlanner-Info.plist
      properties:
        UILaunchScreen: {}
    settings:
      base:
        PRODUCT_BUNDLE_IDENTIFIER: dev.ambrosiaforme.mealplanner
        CODE_SIGN_STYLE: Automatic
        CODE_SIGNING_REQUIRED: NO
        CODE_SIGNING_ALLOWED: NO
        SWIFT_VERSION: "6.0"
  MealPlannerUITests:
    type: bundle.ui-testing
    platform: iOS
    sources:
      - MealPlannerUITests
    dependencies:
      - target: MealPlanner
    settings:
      base:
        CODE_SIGNING_REQUIRED: NO
        CODE_SIGNING_ALLOWED: NO
schemes:
  MealPlanner:
    build:
      targets:
        MealPlanner: all
        MealPlannerUITests: [test]
    test:
      targets:
        - MealPlannerUITests
    run:
      config: Debug
```

- [ ] **Step 5: Add the thin app target's source**

Create `ios/MealPlanner/MealPlannerApp.swift`:

```swift
import SwiftUI
import AppCore

@main
struct MealPlannerApp: App {
    var body: some Scene {
        WindowGroup {
            Text("MealPlanner")
        }
    }
}
```

(`import AppCore` is unused beyond proving the package link works end to end; Task 5 replaces the body with the real root view.)

Create `ios/MealPlanner/Assets.xcassets/Contents.json`:

```json
{
  "info" : {
    "author" : "xcode",
    "version" : 1
  }
}
```

Create `ios/MealPlanner/Assets.xcassets/AppIcon.appiconset/Contents.json`:

```json
{
  "images" : [
    {
      "idiom" : "universal",
      "platform" : "ios",
      "size" : "1024x1024"
    }
  ],
  "info" : {
    "author" : "xcode",
    "version" : 1
  }
}
```

Create `ios/MealPlanner/Assets.xcassets/AccentColor.colorset/Contents.json`:

```json
{
  "colors" : [
    {
      "idiom" : "universal"
    }
  ],
  "info" : {
    "author" : "xcode",
    "version" : 1
  }
}
```

Create `ios/MealPlannerUITests/SmokeUITests.swift` (a minimal, real test — proves the UI test target and app launch work; Task 7 replaces it with the sign-in flow):

```swift
import XCTest

final class SmokeUITests: XCTestCase {
    func testAppLaunches() throws {
        let app = XCUIApplication()
        app.launch()
        XCTAssertTrue(app.staticTexts["MealPlanner"].waitForExistence(timeout: 5))
    }
}
```

- [ ] **Step 6: Generate the Xcode project and build it**

```bash
cd ios && xcodegen generate
xcodebuild -project MealPlanner.xcodeproj -scheme MealPlanner -destination 'generic/platform=iOS Simulator' build
```

Expected: `** BUILD SUCCEEDED **`.

- [ ] **Step 7: Run the UI smoke test on a simulator**

```bash
cd ios
DEVICE_UDID=$(xcrun simctl list devices available -j | /usr/bin/python3 -c "
import json, sys
data = json.load(sys.stdin)['devices']
for runtime, devices in data.items():
    if 'iOS' in runtime:
        for d in devices:
            if d['name'].startswith('iPhone'):
                print(d['udid']); raise SystemExit
")
xcodebuild -project MealPlanner.xcodeproj -scheme MealPlanner -destination "id=$DEVICE_UDID" test
```

Expected: `** TEST SUCCEEDED **`. (This dynamic simulator lookup, rather than a hardcoded device name, is reused in Task 7's CI job so it keeps working as Xcode's bundled simulator list changes across macOS runner images.)

- [ ] **Step 8: Ignore generated and build artifacts**

Add to the root `.gitignore`, inside the existing `# iOS` section:

```gitignore
ios/*.xcodeproj/
ios/MealPlanner/Generated/
ios/MealPlannerKit/.build/
ios/MealPlannerKit/.swiftpm/
```

- [ ] **Step 9: Commit**

```bash
git add ios/project.yml ios/MealPlanner ios/MealPlannerUITests ios/MealPlannerKit/Package.swift ios/MealPlannerKit/Sources/AppCore/Placeholder.swift .gitignore
git commit -m "ios: scaffold XcodeGen project and MealPlannerKit package"
```

---

## Task 2: Swift OpenAPI Generator wiring and drift check

**Files:**
- Create: `ios/MealPlannerKit/Sources/API/openapi.yaml` (symlink to the repo-root spec)
- Create: `ios/MealPlannerKit/Sources/API/openapi-generator-config.yaml`
- Create: `ios/MealPlannerKit/Sources/API/GeneratedSources/*.swift` (command-plugin output, committed)
- Create: `ios/MealPlannerKit/Sources/API/APIClient.swift`
- Modify: `ios/MealPlannerKit/Package.swift`
- Modify: `Makefile`

**Interfaces:**
- Consumes: nothing from Task 1 beyond the package shell.
- Produces: `APIClient.makeClient(baseURL:accessTokenProvider:)` — actually superseded once Task 3 exists; this task exposes the raw pieces later tasks assemble: `Components.Schemas.*` and `Operations.*` (generated), and `API.makeAuthlessClient(baseURL:) -> Client` (a `Client` with no middleware, used by `refreshSession` in Task 3 to avoid recursive auth).

- [ ] **Step 1: Add the `API` target and its dependencies**

Replace the contents of `ios/MealPlannerKit/Package.swift`:

```swift
// swift-tools-version:6.2
import PackageDescription

let package = Package(
    name: "MealPlannerKit",
    // iOS 26+ per the parent spec's locked decisions. macOS is also declared, only so
    // `swift test` can run natively on the build machine without a simulator (spec §4).
    platforms: [.iOS(.v26), .macOS(.v15)],
    products: [
        .library(name: "MealPlannerKit", targets: ["AppCore"])
    ],
    dependencies: [
        .package(url: "https://github.com/apple/swift-openapi-generator", from: "1.6.0"),
        .package(url: "https://github.com/apple/swift-openapi-runtime", from: "1.7.0"),
        .package(url: "https://github.com/apple/swift-openapi-urlsession", from: "1.0.0"),
        .package(url: "https://github.com/apple/swift-http-types", from: "1.0.0"),
    ],
    targets: [
        .target(
            name: "API",
            dependencies: [
                .product(name: "OpenAPIRuntime", package: "swift-openapi-runtime"),
                .product(name: "OpenAPIURLSession", package: "swift-openapi-urlsession"),
            ]
        ),
        .target(name: "AppCore")
    ]
)
```

- [ ] **Step 2: Symlink the root OpenAPI document into the target**

The generator's config file and OpenAPI document must live directly in the target's source directory (`Sources/API/`). Symlink rather than copy, so the client can never silently drift from the contract file it's generated from:

```bash
cd ios/MealPlannerKit/Sources/API
ln -s ../../../../openapi.yaml openapi.yaml
cd /Users/mkajzik/wlasne/ambrosia-for-me
readlink ios/MealPlannerKit/Sources/API/openapi.yaml
```

Expected: prints `../../../../openapi.yaml`, and `cat ios/MealPlannerKit/Sources/API/openapi.yaml | head -3` shows the same `openapi: 3.0.3` header as the root file.

- [ ] **Step 3: Write the generator config**

Create `ios/MealPlannerKit/Sources/API/openapi-generator-config.yaml`:

```yaml
generate:
  - types
  - client
accessModifier: public
namingStrategy: idiomatic
```

(`idiomatic` naming turns `display_name`/`access_token`/etc. into `displayName`/`accessToken`; verified against this repo's full `openapi.yaml` to produce no naming conflicts.)

- [ ] **Step 4: Generate and commit the client**

```bash
cd ios/MealPlannerKit
swift package --allow-writing-to-package-directory generate-code-from-openapi
```

Expected: ends with `✅ OpenAPI code generation for target 'API' successfully completed.` and creates `Sources/API/GeneratedSources/{Client,Types,Types+Components,Types+Operations,Types+Components+Schemas,Types+Components+Parameters,Types+Components+RequestBodies,Types+Components+Responses,Types+Components+Headers}.swift`.

- [ ] **Step 5: Add the thin client-construction helper**

Create `ios/MealPlannerKit/Sources/API/APIClient.swift`:

```swift
import Foundation
import OpenAPIRuntime
import OpenAPIURLSession

public enum APIEnvironment {
    /// Matches `openapi.yaml`'s `servers[0]`. Overridable for a device pointed at a different host.
    public static var baseURL = URL(string: "http://localhost:8080/v1")!
}

/// Builds a `Client` with no middleware. Used only for the one call that must never recurse
/// into the auth middleware: exchanging a refresh token for a new access token (Task 3).
public func makeAuthlessClient(baseURL: URL = APIEnvironment.baseURL) -> Client {
    Client(serverURL: baseURL, transport: URLSessionTransport())
}

/// Builds a `Client` with the given middlewares (Task 3 supplies the bearer-auth middleware).
public func makeClient(baseURL: URL = APIEnvironment.baseURL, middlewares: [any ClientMiddleware]) -> Client {
    Client(serverURL: baseURL, transport: URLSessionTransport(), middlewares: middlewares)
}
```

- [ ] **Step 6: Verify the package builds against the generated client**

Run: `cd ios/MealPlannerKit && swift build`
Expected: `Build complete!`.

- [ ] **Step 7: Add Makefile targets for generation and drift checking**

Modify `Makefile`. Add near the existing `generate-web`/`check-generated-web` block:

```makefile
GENERATED_IOS := ios/MealPlannerKit/Sources/API/GeneratedSources

.PHONY: generate-ios check-generated-ios build-ios test-ios

generate-ios: ## Regenerate the iOS Swift OpenAPI client from openapi.yaml
	cd ios/MealPlannerKit && swift package --allow-writing-to-package-directory generate-code-from-openapi

check-generated-ios: generate-ios ## Fail if the committed iOS API client is out of date
	git add -AN -- $(GENERATED_IOS)
	git diff --exit-code -- $(GENERATED_IOS)

build-ios: ## Build the iOS app for the simulator (needs Xcode and XcodeGen)
	cd ios && xcodegen generate && xcodebuild -project MealPlanner.xcodeproj -scheme MealPlanner -destination 'generic/platform=iOS Simulator' build

test-ios: ## Run the iOS unit tests (Swift Testing, no Xcode or simulator needed)
	cd ios/MealPlannerKit && swift test
```

Also update the `.PHONY` line and `check` target's dependency list and comment:

```makefile
.PHONY: help lint-api test-backend lint-backend lint-web test-web e2e-web run-web generate generate-web generate-ios check-generated check-generated-web check-generated-ios build-ios test-ios migrate run-api import-usda db-up db-down check
```

```makefile
check: lint-api test-backend lint-backend check-generated lint-web test-web test-ios ## Everything CI runs, except the compose workflow, the web E2E flows and the iOS UI tests
```

- [ ] **Step 8: Verify the new Makefile targets**

```bash
make check-generated-ios
```

Expected: exits 0 with no diff output (the just-generated client matches what was committed in Step 4).

- [ ] **Step 9: Commit**

```bash
git add ios/MealPlannerKit Makefile
git commit -m "ios: wire Swift OpenAPI Generator against openapi.yaml"
```

---

## Task 3: Keychain token storage, single-flight refresh, and the bearer middleware

**Files:**
- Create: `ios/MealPlannerKit/Sources/Auth/TokenStore.swift`
- Create: `ios/MealPlannerKit/Sources/Auth/KeychainTokenStore.swift`
- Create: `ios/MealPlannerKit/Sources/Auth/TokenRefresher.swift`
- Create: `ios/MealPlannerKit/Sources/Auth/BearerAuthMiddleware.swift`
- Create: `ios/MealPlannerKit/Sources/Auth/AuthError.swift`
- Create: `ios/MealPlannerKit/Tests/MealPlannerKitTests/InMemoryTokenStore.swift`
- Create: `ios/MealPlannerKit/Tests/MealPlannerKitTests/StubTransport.swift`
- Create: `ios/MealPlannerKit/Tests/MealPlannerKitTests/TokenRefresherTests.swift`
- Create: `ios/MealPlannerKit/Tests/MealPlannerKitTests/KeychainTokenStoreTests.swift`
- Modify: `ios/MealPlannerKit/Package.swift`

**Interfaces:**
- Consumes: `Client`, `Components.Schemas.RefreshRequest/AuthResponse`, `Operations.RefreshSession` from the `API` target (Task 2); `makeAuthlessClient(baseURL:)`.
- Produces: `protocol TokenStore` (`accessToken`, `refreshToken`, `save(accessToken:refreshToken:)`, `clear()`), `KeychainTokenStore: TokenStore`, `actor TokenRefresher` with `func currentAccessToken() -> String?` and `func refreshAccessToken() async throws -> String`, `struct BearerAuthMiddleware: ClientMiddleware` (used by Task 4's client), `enum AuthError`. Task 4 consumes all of these.

- [ ] **Step 1: Add the `Auth` target and a test target**

Modify `ios/MealPlannerKit/Package.swift` — add to `targets:`:

```swift
        .target(
            name: "Auth",
            dependencies: [
                "API",
                .product(name: "OpenAPIRuntime", package: "swift-openapi-runtime"),
                .product(name: "HTTPTypes", package: "swift-http-types"),
            ]
        ),
```

and, after the `AppCore` target, add a test target:

```swift
        .testTarget(
            name: "MealPlannerKitTests",
            dependencies: ["API", "Auth"]
        ),
```

- [ ] **Step 2: Define the token store protocol and error type**

Create `ios/MealPlannerKit/Sources/Auth/TokenStore.swift`:

```swift
public protocol TokenStore: Sendable {
    var accessToken: String? { get }
    var refreshToken: String? { get }
    func save(accessToken: String, refreshToken: String)
    func clear()
}
```

Create `ios/MealPlannerKit/Sources/Auth/AuthError.swift`:

```swift
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
```

- [ ] **Step 3: Write the failing single-flight refresh test**

Create `ios/MealPlannerKit/Tests/MealPlannerKitTests/InMemoryTokenStore.swift`:

```swift
import Auth

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
```

Create `ios/MealPlannerKit/Tests/MealPlannerKitTests/StubTransport.swift`:

```swift
import Foundation
import HTTPTypes
import OpenAPIRuntime

/// A `ClientTransport` that counts calls and returns a canned response, so tests can assert
/// on how many real HTTP calls a piece of logic made without touching the network.
actor StubTransport: ClientTransport {
    private(set) var callCount = 0
    private let makeResponse: @Sendable () async throws -> (status: Int, jsonBody: String)

    init(makeResponse: @escaping @Sendable () async throws -> (status: Int, jsonBody: String)) {
        self.makeResponse = makeResponse
    }

    func send(
        _ request: HTTPRequest,
        body: HTTPBody?,
        baseURL: URL,
        operationID: String
    ) async throws -> (HTTPResponse, HTTPBody?) {
        callCount += 1
        let (status, jsonBody) = try await makeResponse()
        let response = HTTPResponse(
            status: .init(code: status),
            headerFields: [.contentType: "application/json"]
        )
        return (response, HTTPBody(jsonBody))
    }
}
```

Create `ios/MealPlannerKit/Tests/MealPlannerKitTests/TokenRefresherTests.swift`:

```swift
import Testing
import API
@testable import Auth

@Suite
struct TokenRefresherTests {
    static let authResponseJSON = """
    {
      "access_token": "new-access-token",
      "refresh_token": "new-refresh-token",
      "token_type": "Bearer",
      "expires_in": 900,
      "user": {
        "id": "11111111-1111-1111-1111-111111111111",
        "email": "person@example.com",
        "display_name": "Person",
        "created_at": "2026-01-01T00:00:00Z",
        "updated_at": "2026-01-01T00:00:00Z"
      }
    }
    """

    @Test("Concurrent refreshes make exactly one network call and all callers get the new token")
    func singleFlight() async throws {
        let transport = StubTransport { (200, Self.authResponseJSON) }
        let client = makeAuthlessClient(transport: transport)
        let tokenStore = InMemoryTokenStore(accessToken: "old-access-token", refreshToken: "old-refresh-token")
        let refresher = TokenRefresher(refreshClient: client, tokenStore: tokenStore)

        async let first = refresher.refreshAccessToken()
        async let second = refresher.refreshAccessToken()
        async let third = refresher.refreshAccessToken()
        let results = try await [first, second, third]

        #expect(results == ["new-access-token", "new-access-token", "new-access-token"])
        #expect(await transport.callCount == 1)
        #expect(tokenStore.accessToken == "new-access-token")
        #expect(tokenStore.refreshToken == "new-refresh-token")
    }

    @Test("A rejected refresh token clears the Keychain and reports signed out")
    func rejectedRefreshToken() async throws {
        let transport = StubTransport {
            (401, #"{"type":"about:blank","title":"Unauthorized","status":401,"code":"unauthorized"}"#)
        }
        let client = makeAuthlessClient(transport: transport)
        let tokenStore = InMemoryTokenStore(accessToken: "old-access-token", refreshToken: "used-refresh-token")
        let refresher = TokenRefresher(refreshClient: client, tokenStore: tokenStore)

        await #expect(throws: AuthError.signedOut) {
            try await refresher.refreshAccessToken()
        }
        #expect(tokenStore.accessToken == nil)
        #expect(tokenStore.refreshToken == nil)
    }

    @Test("No stored refresh token reports signed out without a network call")
    func noRefreshToken() async throws {
        let transport = StubTransport { (200, Self.authResponseJSON) }
        let client = makeAuthlessClient(transport: transport)
        let tokenStore = InMemoryTokenStore()
        let refresher = TokenRefresher(refreshClient: client, tokenStore: tokenStore)

        await #expect(throws: AuthError.signedOut) {
            try await refresher.refreshAccessToken()
        }
        #expect(await transport.callCount == 0)
    }
}
```

This test needs a `makeAuthlessClient(transport:)` overload that accepts a stub transport (the real one in `API` always builds a `URLSessionTransport`). Add it now so the test compiles before `TokenRefresher` exists:

Modify `ios/MealPlannerKit/Sources/API/APIClient.swift`, adding below the existing `makeAuthlessClient`:

```swift
/// Test-only seam: build a client against an arbitrary transport instead of `URLSessionTransport`.
public func makeAuthlessClient(baseURL: URL = APIEnvironment.baseURL, transport: any ClientTransport) -> Client {
    Client(serverURL: baseURL, transport: transport)
}
```

- [ ] **Step 4: Run the tests to confirm they fail**

Run: `cd ios/MealPlannerKit && swift test --filter TokenRefresherTests`
Expected: FAIL to compile — `TokenRefresher` does not exist yet.

- [ ] **Step 5: Implement `TokenRefresher`**

Create `ios/MealPlannerKit/Sources/Auth/TokenRefresher.swift`:

```swift
import API

/// Single-flights concurrent refresh attempts so that N requests racing an expired access
/// token trigger exactly one `POST /auth/refresh` call, matching the backend rule that
/// replaying an already-used refresh token revokes the whole session family
/// (`backend/CLAUDE.md`): if every caller refreshed independently, the first response would
/// invalidate the token the others are about to present, and they would all be signed out.
public actor TokenRefresher {
    private let refreshClient: Client
    private let tokenStore: any TokenStore
    private var inFlight: Task<String, Error>?

    public init(refreshClient: Client, tokenStore: any TokenStore) {
        self.refreshClient = refreshClient
        self.tokenStore = tokenStore
    }

    public func currentAccessToken() -> String? {
        tokenStore.accessToken
    }

    public func refreshAccessToken() async throws -> String {
        if let inFlight {
            return try await inFlight.value
        }
        let task = Task { try await performRefresh() }
        inFlight = task
        defer { inFlight = nil }
        return try await task.value
    }

    private func performRefresh() async throws -> String {
        guard let refreshToken = tokenStore.refreshToken else {
            throw AuthError.signedOut
        }
        let response = try await refreshClient.refreshSession(
            .init(body: .json(.init(refreshToken: refreshToken)))
        )
        switch response {
        case .ok(let ok):
            let auth = try ok.body.json
            tokenStore.save(accessToken: auth.accessToken, refreshToken: auth.refreshToken)
            return auth.accessToken
        case .badRequest, .unauthorized:
            tokenStore.clear()
            throw AuthError.signedOut
        case .tooManyRequests:
            throw AuthError.rateLimited
        case .internalServerError:
            throw AuthError.server("The server had a problem refreshing the session.")
        case .undocumented(let statusCode, _):
            throw AuthError.server("Unexpected response (\(statusCode)) while refreshing the session.")
        }
    }
}
```

- [ ] **Step 6: Run the tests to confirm they pass**

Run: `cd ios/MealPlannerKit && swift test --filter TokenRefresherTests`
Expected: 3 tests pass.

- [ ] **Step 7: Write the bearer middleware**

Create `ios/MealPlannerKit/Sources/Auth/BearerAuthMiddleware.swift`:

```swift
import Foundation
import HTTPTypes
import OpenAPIRuntime

/// Attaches the current access token to every request, and on a `401`, refreshes once
/// (single-flighted by `TokenRefresher`) and retries the request exactly once with the new
/// token. Never retries a request whose body has already been consumed by a failed attempt.
public struct BearerAuthMiddleware: ClientMiddleware {
    private let refresher: TokenRefresher

    public init(refresher: TokenRefresher) {
        self.refresher = refresher
    }

    public func intercept(
        _ request: HTTPRequest,
        body: HTTPBody?,
        baseURL: URL,
        operationID: String,
        next: (HTTPRequest, HTTPBody?, URL) async throws -> (HTTPResponse, HTTPBody?)
    ) async throws -> (HTTPResponse, HTTPBody?) {
        var request = request
        if let token = await refresher.currentAccessToken() {
            request.headerFields[.authorization] = "Bearer \(token)"
        }
        let (response, responseBody) = try await next(request, body, baseURL)
        guard response.status.code == 401 else {
            return (response, responseBody)
        }
        if let body, body.iterationBehavior != .multiple {
            // The body was a single-use stream already consumed by the first attempt; a retry
            // would send an empty body, so give up rather than send a broken request.
            return (response, responseBody)
        }
        guard let newToken = try? await refresher.refreshAccessToken() else {
            return (response, responseBody)
        }
        var retried = request
        retried.headerFields[.authorization] = "Bearer \(newToken)"
        return try await next(retried, body, baseURL)
    }
}
```

- [ ] **Step 8: Write and implement the Keychain store, with a smoke test**

Create `ios/MealPlannerKit/Sources/Auth/KeychainTokenStore.swift`:

```swift
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
        if SecItemCopyMatching(query as CFDictionary, nil) == errSecSuccess {
            SecItemUpdate(query as CFDictionary, [kSecValueData as String: data] as CFDictionary)
        } else {
            var addQuery = query
            addQuery[kSecValueData as String] = data
            addQuery[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
            SecItemAdd(addQuery as CFDictionary, nil)
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
```

Create `ios/MealPlannerKit/Tests/MealPlannerKitTests/KeychainTokenStoreTests.swift`:

```swift
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
```

- [ ] **Step 9: Run the full Auth test suite**

Run: `cd ios/MealPlannerKit && swift test`
Expected: all tests pass (`TokenRefresherTests` and `KeychainTokenStoreTests`).

- [ ] **Step 10: Commit**

```bash
git add ios/MealPlannerKit
git commit -m "ios: add Keychain token store, single-flight refresh, bearer middleware"
```

---

## Task 4: AuthRepository (register, login, logout)

**Files:**
- Create: `ios/MealPlannerKit/Sources/Auth/AuthRepository.swift`
- Create: `ios/MealPlannerKit/Tests/MealPlannerKitTests/AuthRepositoryTests.swift`

**Interfaces:**
- Consumes: `Client`, `Operations.RegisterUser/LoginUser/LogoutUser/GetMe`, `Components.Schemas.{RegisterRequest,LoginRequest,RefreshRequest,User}` from `API`; `TokenStore`, `AuthError` from `Auth` (Task 3).
- Produces: `struct AuthRepository` with `func register(email:password:displayName:) async throws -> Components.Schemas.User`, `func login(email:password:) async throws -> Components.Schemas.User`, `func logout() async`, `func currentUser() async throws -> Components.Schemas.User`. Task 5's `AuthViewModel` and `AppState` consume all four.

- [ ] **Step 1: Write the failing tests**

Create `ios/MealPlannerKit/Tests/MealPlannerKitTests/AuthRepositoryTests.swift`:

```swift
import Testing
import API
@testable import Auth

@Suite
struct AuthRepositoryTests {
    static func authResponseJSON(access: String = "a1", refresh: String = "r1") -> String {
        """
        {
          "access_token": "\(access)",
          "refresh_token": "\(refresh)",
          "token_type": "Bearer",
          "expires_in": 900,
          "user": {
            "id": "11111111-1111-1111-1111-111111111111",
            "email": "person@example.com",
            "display_name": "Person",
            "created_at": "2026-01-01T00:00:00Z",
            "updated_at": "2026-01-01T00:00:00Z"
          }
        }
        """
    }

    @Test("Registering saves the returned tokens and returns the user")
    func registerSuccess() async throws {
        let transport = StubTransport { (201, Self.authResponseJSON()) }
        let tokenStore = InMemoryTokenStore()
        let repository = AuthRepository(
            client: makeAuthlessClient(transport: transport),
            tokenStore: tokenStore
        )

        let user = try await repository.register(email: "person@example.com", password: "correct-horse-battery", displayName: "Person")

        #expect(user.email == "person@example.com")
        #expect(tokenStore.accessToken == "a1")
        #expect(tokenStore.refreshToken == "r1")
    }

    @Test("Registering with a taken email throws emailTaken")
    func registerEmailTaken() async throws {
        let transport = StubTransport {
            (409, #"{"type":"about:blank","title":"Conflict","status":409,"code":"email_taken"}"#)
        }
        let repository = AuthRepository(
            client: makeAuthlessClient(transport: transport),
            tokenStore: InMemoryTokenStore()
        )

        await #expect(throws: AuthError.emailTaken) {
            try await repository.register(email: "person@example.com", password: "correct-horse-battery", displayName: "Person")
        }
    }

    @Test("Registering with an invalid field throws validationFailed with the problem's detail")
    func registerValidationFailed() async throws {
        let transport = StubTransport {
            (400, #"{"type":"about:blank","title":"Bad Request","status":400,"detail":"password is too short","code":"validation_failed"}"#)
        }
        let repository = AuthRepository(
            client: makeAuthlessClient(transport: transport),
            tokenStore: InMemoryTokenStore()
        )

        await #expect(throws: AuthError.validationFailed("password is too short")) {
            try await repository.register(email: "person@example.com", password: "short", displayName: "Person")
        }
    }

    @Test("Signing in saves the returned tokens and returns the user")
    func loginSuccess() async throws {
        let transport = StubTransport { (200, Self.authResponseJSON()) }
        let tokenStore = InMemoryTokenStore()
        let repository = AuthRepository(
            client: makeAuthlessClient(transport: transport),
            tokenStore: tokenStore
        )

        let user = try await repository.login(email: "person@example.com", password: "correct-horse-battery")

        #expect(user.displayName == "Person")
        #expect(tokenStore.accessToken == "a1")
    }

    @Test("Wrong password or unknown email both throw the same invalidCredentials error")
    func loginInvalidCredentials() async throws {
        let transport = StubTransport {
            (401, #"{"type":"about:blank","title":"Unauthorized","status":401,"code":"unauthorized"}"#)
        }
        let repository = AuthRepository(
            client: makeAuthlessClient(transport: transport),
            tokenStore: InMemoryTokenStore()
        )

        await #expect(throws: AuthError.invalidCredentials) {
            try await repository.login(email: "nobody@example.com", password: "wrong-password")
        }
    }

    @Test("Logging out revokes the session and clears the Keychain even if the network call fails")
    func logoutClearsLocalStateOnFailure() async throws {
        let transport = StubTransport { (500, #"{"type":"about:blank","title":"Error","status":500,"code":"internal_error"}"#) }
        let tokenStore = InMemoryTokenStore(accessToken: "a1", refreshToken: "r1")
        let repository = AuthRepository(
            client: makeAuthlessClient(transport: transport),
            tokenStore: tokenStore
        )

        await repository.logout()

        #expect(tokenStore.accessToken == nil)
        #expect(tokenStore.refreshToken == nil)
    }
}
```

- [ ] **Step 2: Run the tests to confirm they fail**

Run: `cd ios/MealPlannerKit && swift test --filter AuthRepositoryTests`
Expected: FAIL to compile — `AuthRepository` does not exist yet.

- [ ] **Step 3: Implement `AuthRepository`**

Create `ios/MealPlannerKit/Sources/Auth/AuthRepository.swift`:

```swift
import API

public struct AuthRepository: Sendable {
    private let client: Client
    private let tokenStore: any TokenStore

    public init(client: Client, tokenStore: any TokenStore) {
        self.client = client
        self.tokenStore = tokenStore
    }

    public func register(email: String, password: String, displayName: String) async throws -> Components.Schemas.User {
        let response = try await client.registerUser(
            .init(body: .json(.init(email: email, password: password, displayName: displayName)))
        )
        switch response {
        case .created(let created):
            let auth = try created.body.json
            tokenStore.save(accessToken: auth.accessToken, refreshToken: auth.refreshToken)
            return auth.user
        case .badRequest(let badRequest):
            let problem = try badRequest.body.applicationProblemJson
            throw AuthError.validationFailed(problem.detail ?? problem.title)
        case .conflict:
            throw AuthError.emailTaken
        case .tooManyRequests:
            throw AuthError.rateLimited
        case .internalServerError:
            throw AuthError.server("The server had a problem creating the account.")
        case .undocumented(let statusCode, _):
            throw AuthError.server("Unexpected response (\(statusCode)) while creating the account.")
        }
    }

    public func login(email: String, password: String) async throws -> Components.Schemas.User {
        let response = try await client.loginUser(.init(body: .json(.init(email: email, password: password))))
        switch response {
        case .ok(let ok):
            let auth = try ok.body.json
            tokenStore.save(accessToken: auth.accessToken, refreshToken: auth.refreshToken)
            return auth.user
        case .badRequest(let badRequest):
            let problem = try badRequest.body.applicationProblemJson
            throw AuthError.validationFailed(problem.detail ?? problem.title)
        case .unauthorized:
            // The API answers an unknown email and a wrong password identically
            // (backend/CLAUDE.md); the UI must never imply which one was wrong.
            throw AuthError.invalidCredentials
        case .tooManyRequests:
            throw AuthError.rateLimited
        case .internalServerError:
            throw AuthError.server("The server had a problem signing you in.")
        case .undocumented(let statusCode, _):
            throw AuthError.server("Unexpected response (\(statusCode)) while signing in.")
        }
    }

    public func logout() async {
        if let refreshToken = tokenStore.refreshToken {
            _ = try? await client.logoutUser(.init(body: .json(.init(refreshToken: refreshToken))))
        }
        tokenStore.clear()
    }

    public func currentUser() async throws -> Components.Schemas.User {
        let response = try await client.getMe(.init())
        switch response {
        case .ok(let ok):
            return try ok.body.json
        case .unauthorized:
            throw AuthError.signedOut
        case .tooManyRequests:
            throw AuthError.rateLimited
        case .internalServerError:
            throw AuthError.server("The server had a problem loading your profile.")
        case .undocumented(let statusCode, _):
            throw AuthError.server("Unexpected response (\(statusCode)) while loading your profile.")
        }
    }
}
```

- [ ] **Step 4: Run the tests to confirm they pass**

Run: `cd ios/MealPlannerKit && swift test --filter AuthRepositoryTests`
Expected: all 6 tests pass.

- [ ] **Step 5: Run the whole package test suite**

Run: `cd ios/MealPlannerKit && swift test`
Expected: all tests across `TokenRefresherTests`, `KeychainTokenStoreTests` and `AuthRepositoryTests` pass.

- [ ] **Step 6: Commit**

```bash
git add ios/MealPlannerKit
git commit -m "ios: add AuthRepository for register, login, logout and profile fetch"
```

---

## Task 5: App state, sign-in/register views, and the five-tab shell

**Files:**
- Create: `ios/MealPlannerKit/Sources/Features/Auth/AuthViewModel.swift`
- Create: `ios/MealPlannerKit/Sources/Features/Auth/SignInView.swift`
- Create: `ios/MealPlannerKit/Sources/Features/Auth/RegisterView.swift`
- Create: `ios/MealPlannerKit/Sources/Features/Today/TodayView.swift`
- Create: `ios/MealPlannerKit/Sources/Features/Plan/PlanView.swift`
- Create: `ios/MealPlannerKit/Sources/Features/Meals/MealsView.swift`
- Create: `ios/MealPlannerKit/Sources/Features/Shopping/ShoppingView.swift`
- Create: `ios/MealPlannerKit/Sources/Features/Profile/ProfileView.swift`
- Create: `ios/MealPlannerKit/Sources/AppCore/AppState.swift`
- Create: `ios/MealPlannerKit/Sources/AppCore/RootView.swift`
- Create: `ios/MealPlannerKit/Sources/AppCore/TabShellView.swift`
- Create: `ios/MealPlannerKit/Tests/MealPlannerKitTests/AuthViewModelTests.swift`
- Delete: `ios/MealPlannerKit/Sources/AppCore/Placeholder.swift`
- Modify: `ios/MealPlannerKit/Package.swift`
- Modify: `ios/MealPlanner/MealPlannerApp.swift`

**Interfaces:**
- Consumes: `AuthRepository`, `AuthError` from `Auth` (Task 4); `makeClient`, `makeAuthlessClient`, `APIEnvironment` from `API` (Task 2); `KeychainTokenStore`, `TokenRefresher`, `BearerAuthMiddleware` from `Auth` (Task 3).
- Produces: `@MainActor @Observable public final class AppState` with `enum Session { case signedOut, signedIn(Components.Schemas.User) }`, `var session: Session`, `func restoreSession() async`, `func signIn(email:password:) async throws`, `func register(email:password:displayName:) async throws`, `func signOut() async`; `public struct RootView: View` (the package's single public entry point, used by the app target). Task 7's XCUITest drives `RootView` end to end by launching the app.

- [ ] **Step 1: Add the `Features` target and wire `AppCore`'s real dependencies**

Modify `ios/MealPlannerKit/Package.swift` — add to `targets:`:

```swift
        .target(name: "Features", dependencies: ["Auth"]),
```

and change the `AppCore` target from `.target(name: "AppCore")` to:

```swift
        .target(name: "AppCore", dependencies: ["Auth", "Features"]),
```

Delete `ios/MealPlannerKit/Sources/AppCore/Placeholder.swift` (its job — proving the package links into the app target — is now done by the real `RootView`).

- [ ] **Step 2: Write the failing `AuthViewModel` tests**

Create `ios/MealPlannerKit/Tests/MealPlannerKitTests/AuthViewModelTests.swift`:

```swift
import Testing
import API
@testable import Auth
@testable import Features

@Suite
@MainActor
struct AuthViewModelTests {
    @Test("A successful sign-in reports the signed-in user and clears any error")
    func signInSuccess() async throws {
        let transport = StubTransport {
            (200, """
            {
              "access_token": "a1", "refresh_token": "r1", "token_type": "Bearer", "expires_in": 900,
              "user": {
                "id": "11111111-1111-1111-1111-111111111111", "email": "person@example.com",
                "display_name": "Person", "created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z"
              }
            }
            """)
        }
        let repository = AuthRepository(client: makeAuthlessClient(transport: transport), tokenStore: InMemoryTokenStore())
        let viewModel = AuthViewModel(authRepository: repository)

        viewModel.email = "person@example.com"
        viewModel.password = "correct-horse-battery"
        let user = await viewModel.signIn()

        #expect(user?.email == "person@example.com")
        #expect(viewModel.errorMessage == nil)
        #expect(viewModel.isSubmitting == false)
    }

    @Test("Invalid credentials set a generic error message, never distinguishing email from password")
    func signInInvalidCredentials() async throws {
        let transport = StubTransport {
            (401, #"{"type":"about:blank","title":"Unauthorized","status":401,"code":"unauthorized"}"#)
        }
        let repository = AuthRepository(client: makeAuthlessClient(transport: transport), tokenStore: InMemoryTokenStore())
        let viewModel = AuthViewModel(authRepository: repository)

        viewModel.email = "nobody@example.com"
        viewModel.password = "wrong"
        let user = await viewModel.signIn()

        #expect(user == nil)
        #expect(viewModel.errorMessage == "Invalid email or password.")
    }

    @Test("A taken email sets a distinct message on the register flow")
    func registerEmailTaken() async throws {
        let transport = StubTransport {
            (409, #"{"type":"about:blank","title":"Conflict","status":409,"code":"email_taken"}"#)
        }
        let repository = AuthRepository(client: makeAuthlessClient(transport: transport), tokenStore: InMemoryTokenStore())
        let viewModel = AuthViewModel(authRepository: repository)

        viewModel.email = "taken@example.com"
        viewModel.password = "correct-horse-battery"
        viewModel.displayName = "Person"
        let user = await viewModel.register()

        #expect(user == nil)
        #expect(viewModel.errorMessage == "That email is already registered.")
    }
}
```

- [ ] **Step 3: Run the tests to confirm they fail**

Run: `cd ios/MealPlannerKit && swift test --filter AuthViewModelTests`
Expected: FAIL to compile — `AuthViewModel` does not exist yet.

- [ ] **Step 4: Implement `AuthViewModel`**

Create `ios/MealPlannerKit/Sources/Features/Auth/AuthViewModel.swift`:

```swift
import API
import Auth
import Observation

@Observable
@MainActor
public final class AuthViewModel {
    public var email = ""
    public var password = ""
    public var displayName = ""
    public private(set) var isSubmitting = false
    public private(set) var errorMessage: String?

    private let authRepository: AuthRepository

    public init(authRepository: AuthRepository) {
        self.authRepository = authRepository
    }

    public func signIn() async -> Components.Schemas.User? {
        await submit { try await authRepository.login(email: email, password: password) }
    }

    public func register() async -> Components.Schemas.User? {
        await submit {
            try await authRepository.register(email: email, password: password, displayName: displayName)
        }
    }

    private func submit(_ action: () async throws -> Components.Schemas.User) async -> Components.Schemas.User? {
        isSubmitting = true
        errorMessage = nil
        defer { isSubmitting = false }
        do {
            return try await action()
        } catch let error as AuthError {
            errorMessage = message(for: error)
            return nil
        } catch {
            errorMessage = "Something went wrong. Please try again."
            return nil
        }
    }

    private func message(for error: AuthError) -> String {
        switch error {
        case .invalidCredentials:
            return "Invalid email or password."
        case .emailTaken:
            return "That email is already registered."
        case .validationFailed(let detail):
            return detail
        case .rateLimited:
            return "Too many attempts. Please wait a moment and try again."
        case .server(let detail):
            return detail
        case .signedOut:
            return "Please sign in again."
        }
    }
}
```

- [ ] **Step 5: Run the tests to confirm they pass**

Run: `cd ios/MealPlannerKit && swift test --filter AuthViewModelTests`
Expected: all 3 tests pass.

- [ ] **Step 6: Build the sign-in and register views**

Create `ios/MealPlannerKit/Sources/Features/Auth/SignInView.swift`:

```swift
import SwiftUI

public struct SignInView: View {
    @Bindable var viewModel: AuthViewModel
    let onSignedIn: () -> Void
    let onShowRegister: () -> Void

    public init(viewModel: AuthViewModel, onSignedIn: @escaping () -> Void, onShowRegister: @escaping () -> Void) {
        self.viewModel = viewModel
        self.onSignedIn = onSignedIn
        self.onShowRegister = onShowRegister
    }

    public var body: some View {
        Form {
            Section {
                TextField("Email", text: $viewModel.email)
                    .textContentType(.emailAddress)
                    .keyboardType(.emailAddress)
                    .autocorrectionDisabled()
                    .textInputAutocapitalization(.never)
                    .accessibilityIdentifier("signInEmailField")
                SecureField("Password", text: $viewModel.password)
                    .textContentType(.password)
                    .accessibilityIdentifier("signInPasswordField")
            }
            if let errorMessage = viewModel.errorMessage {
                Text(errorMessage)
                    .foregroundStyle(.red)
                    .accessibilityIdentifier("signInErrorMessage")
            }
            Button("Sign In") {
                Task {
                    if await viewModel.signIn() != nil {
                        onSignedIn()
                    }
                }
            }
            .disabled(viewModel.isSubmitting || viewModel.email.isEmpty || viewModel.password.isEmpty)
            .accessibilityIdentifier("signInSubmitButton")
            Button("Create an account", action: onShowRegister)
                .accessibilityIdentifier("showRegisterButton")
        }
        .navigationTitle("Sign In")
    }
}
```

Create `ios/MealPlannerKit/Sources/Features/Auth/RegisterView.swift`:

```swift
import SwiftUI

public struct RegisterView: View {
    @Bindable var viewModel: AuthViewModel
    let onRegistered: () -> Void

    public init(viewModel: AuthViewModel, onRegistered: @escaping () -> Void) {
        self.viewModel = viewModel
        self.onRegistered = onRegistered
    }

    public var body: some View {
        Form {
            Section {
                TextField("Display name", text: $viewModel.displayName)
                    .accessibilityIdentifier("registerDisplayNameField")
                TextField("Email", text: $viewModel.email)
                    .textContentType(.emailAddress)
                    .keyboardType(.emailAddress)
                    .autocorrectionDisabled()
                    .textInputAutocapitalization(.never)
                    .accessibilityIdentifier("registerEmailField")
                SecureField("Password", text: $viewModel.password)
                    .textContentType(.newPassword)
                    .accessibilityIdentifier("registerPasswordField")
            }
            if let errorMessage = viewModel.errorMessage {
                Text(errorMessage)
                    .foregroundStyle(.red)
                    .accessibilityIdentifier("registerErrorMessage")
            }
            Button("Create Account") {
                Task {
                    if await viewModel.register() != nil {
                        onRegistered()
                    }
                }
            }
            .disabled(
                viewModel.isSubmitting || viewModel.email.isEmpty || viewModel.password.isEmpty
                    || viewModel.displayName.isEmpty
            )
            .accessibilityIdentifier("registerSubmitButton")
        }
        .navigationTitle("Create Account")
    }
}
```

- [ ] **Step 7: Add the five placeholder tab screens**

Create `ios/MealPlannerKit/Sources/Features/Today/TodayView.swift`:

```swift
import SwiftUI

public struct TodayView: View {
    public init() {}
    public var body: some View {
        Text("Today").navigationTitle("Today")
    }
}
```

Create `ios/MealPlannerKit/Sources/Features/Plan/PlanView.swift`:

```swift
import SwiftUI

public struct PlanView: View {
    public init() {}
    public var body: some View {
        Text("Plan").navigationTitle("Plan")
    }
}
```

Create `ios/MealPlannerKit/Sources/Features/Meals/MealsView.swift`:

```swift
import SwiftUI

public struct MealsView: View {
    public init() {}
    public var body: some View {
        Text("Meals").navigationTitle("Meals")
    }
}
```

Create `ios/MealPlannerKit/Sources/Features/Shopping/ShoppingView.swift`:

```swift
import SwiftUI

public struct ShoppingView: View {
    public init() {}
    public var body: some View {
        Text("Shopping").navigationTitle("Shopping")
    }
}
```

Create `ios/MealPlannerKit/Sources/Features/Profile/ProfileView.swift`:

```swift
import SwiftUI
import Auth

public struct ProfileView: View {
    let onSignOut: () -> Void

    public init(onSignOut: @escaping () -> Void) {
        self.onSignOut = onSignOut
    }

    public var body: some View {
        Form {
            Button("Sign Out", role: .destructive, action: onSignOut)
                .accessibilityIdentifier("signOutButton")
        }
        .navigationTitle("Profile")
    }
}
```

- [ ] **Step 8: Implement `AppState`**

Create `ios/MealPlannerKit/Sources/AppCore/AppState.swift`:

```swift
import API
import Auth
import Observation

@Observable
@MainActor
public final class AppState {
    public enum Session {
        case signedOut
        case signedIn(Components.Schemas.User)
    }

    public private(set) var session: Session = .signedOut
    public private(set) var isRestoringSession = true

    private let authRepository: AuthRepository
    private let tokenStore: any TokenStore

    public init(authRepository: AuthRepository, tokenStore: any TokenStore) {
        self.authRepository = authRepository
        self.tokenStore = tokenStore
    }

    /// Called once at launch. If a refresh token is on disk, the app is online-first (spec §5):
    /// it fetches the current profile (transparently refreshing the access token if needed)
    /// before deciding whether the user is really still signed in.
    public func restoreSession() async {
        defer { isRestoringSession = false }
        guard tokenStore.refreshToken != nil else {
            session = .signedOut
            return
        }
        do {
            session = .signedIn(try await authRepository.currentUser())
        } catch {
            session = .signedOut
        }
    }

    public func signIn(email: String, password: String) async throws {
        session = .signedIn(try await authRepository.login(email: email, password: password))
    }

    public func register(email: String, password: String, displayName: String) async throws {
        session = .signedIn(try await authRepository.register(email: email, password: password, displayName: displayName))
    }

    public func signOut() async {
        await authRepository.logout()
        session = .signedOut
    }
}
```

- [ ] **Step 9: Make `AuthViewModel` expose the signed-in user, and add `AppState.adoptSession`**

A successful sign-in or registration runs through `AuthViewModel.signIn()`/`.register()`, which already returns the `Components.Schemas.User` on success — but `RootView` needs a way to feed that result into `AppState.session`, since `AppState` (not `AuthViewModel`) is the one place that decides sign-in vs. tab-shell. Modify `ios/MealPlannerKit/Sources/Features/Auth/AuthViewModel.swift`: add a stored property next to `errorMessage`:

```swift
    public private(set) var lastSignedInUser: Components.Schemas.User?
```

and in `submit(_:)`, replace `return try await action()` with:

```swift
            let user = try await action()
            lastSignedInUser = user
            return user
```

Modify `ios/MealPlannerKit/Sources/AppCore/AppState.swift`, adding a method:

```swift
    public func adoptSession(from viewModel: AuthViewModel) {
        guard let user = viewModel.lastSignedInUser else { return }
        session = .signedIn(user)
    }
```

- [ ] **Step 10: Implement the tab shell and root view**

Create `ios/MealPlannerKit/Sources/AppCore/TabShellView.swift`:

```swift
import SwiftUI
import Features

struct TabShellView: View {
    let appState: AppState

    var body: some View {
        TabView {
            NavigationStack { TodayView() }
                .tabItem { Label("Today", systemImage: "sun.max") }
            NavigationStack { PlanView() }
                .tabItem { Label("Plan", systemImage: "calendar") }
            NavigationStack { MealsView() }
                .tabItem { Label("Meals", systemImage: "fork.knife") }
            NavigationStack { ShoppingView() }
                .tabItem { Label("Shopping", systemImage: "cart") }
            NavigationStack {
                ProfileView(onSignOut: { Task { await appState.signOut() } })
            }
            .tabItem { Label("Profile", systemImage: "person") }
        }
    }
}
```

Create `ios/MealPlannerKit/Sources/AppCore/RootView.swift`. `AppState` and `AuthViewModel` are built from the *same* `AuthRepository` instance so that a successful sign-in's tokens (saved by `AuthRepository`, inside `AuthViewModel.signIn()`) and the `AppState.session` transition (driven by `adoptSession(from:)`) agree about which repository — and therefore which Keychain-backed session — is authoritative:

```swift
import SwiftUI
import Auth
import Features

public struct RootView: View {
    @State private var appState: AppState
    @State private var authViewModel: AuthViewModel
    @State private var showingRegister = false

    public init(baseURL: URL = APIEnvironment.baseURL) {
        let tokenStore = KeychainTokenStore()
        let refresher = TokenRefresher(refreshClient: makeAuthlessClient(baseURL: baseURL), tokenStore: tokenStore)
        let client = makeClient(baseURL: baseURL, middlewares: [BearerAuthMiddleware(refresher: refresher)])
        let authRepository = AuthRepository(client: client, tokenStore: tokenStore)
        _appState = State(initialValue: AppState(authRepository: authRepository, tokenStore: tokenStore))
        _authViewModel = State(initialValue: AuthViewModel(authRepository: authRepository))
    }

    public var body: some View {
        Group {
            if appState.isRestoringSession {
                ProgressView()
            } else {
                switch appState.session {
                case .signedOut:
                    NavigationStack {
                        if showingRegister {
                            RegisterView(
                                viewModel: authViewModel,
                                onRegistered: { appState.adoptSession(from: authViewModel) }
                            )
                        } else {
                            SignInView(
                                viewModel: authViewModel,
                                onSignedIn: { appState.adoptSession(from: authViewModel) },
                                onShowRegister: { showingRegister = true }
                            )
                        }
                    }
                case .signedIn:
                    TabShellView(appState: appState)
                }
            }
        }
        .task { await appState.restoreSession() }
    }
}
```

- [ ] **Step 11: Update the app target to use `RootView`**

Modify `ios/MealPlanner/MealPlannerApp.swift`:

```swift
import SwiftUI
import AppCore

@main
struct MealPlannerApp: App {
    var body: some Scene {
        WindowGroup {
            RootView()
        }
    }
}
```

- [ ] **Step 12: Run the full package test suite**

Run: `cd ios/MealPlannerKit && swift test`
Expected: all tests pass (`TokenRefresherTests`, `KeychainTokenStoreTests`, `AuthRepositoryTests`, `AuthViewModelTests`).

- [ ] **Step 13: Build the app and confirm it launches**

```bash
cd ios && xcodegen generate
DEVICE_UDID=$(xcrun simctl list devices available -j | /usr/bin/python3 -c "
import json, sys
data = json.load(sys.stdin)['devices']
for runtime, devices in data.items():
    if 'iOS' in runtime:
        for d in devices:
            if d['name'].startswith('iPhone'):
                print(d['udid']); raise SystemExit
")
xcrun simctl boot "$DEVICE_UDID" 2>/dev/null || true
xcodebuild -project MealPlanner.xcodeproj -scheme MealPlanner -destination "id=$DEVICE_UDID" build
xcrun simctl install "$DEVICE_UDID" "$(find ~/Library/Developer/Xcode/DerivedData -name 'MealPlanner.app' -path '*Debug-iphonesimulator*' | head -1)"
xcrun simctl launch "$DEVICE_UDID" dev.ambrosiaforme.mealplanner
```

Expected: `** BUILD SUCCEEDED **`, then the launch command prints the process id with no error — this needs a running API to fully exercise sign-in (`make run-api` in a separate terminal, `make db-up` and `make migrate` first if not already done), but the app must at least launch to the sign-in screen without crashing even with no API running.

- [ ] **Step 14: Commit**

```bash
git add ios/MealPlannerKit ios/MealPlanner
git commit -m "ios: add app state, sign-in/register flow, and five-tab shell"
```

---

## Task 6: `ios/CLAUDE.md` and root `CLAUDE.md` updates

**Files:**
- Create: `ios/CLAUDE.md`
- Modify: `CLAUDE.md` (repo root)

**Interfaces:**
- Consumes: nothing (documentation only).
- Produces: nothing consumed by later tasks; this is reference documentation for future agents and contributors.

- [ ] **Step 1: Write `ios/CLAUDE.md`**

Create `ios/CLAUDE.md`:

```markdown
# iOS (SwiftUI)

SwiftUI, iOS 26+, Swift 6 language mode. Design: `docs/superpowers/specs/2026-09-30-ios-app-design.md`. Read before changing auth, networking or caching behaviour.

## Layout

- `project.yml`: XcodeGen spec. Run `xcodegen generate` after editing it (or via `make build-ios`) — the generated `MealPlanner.xcodeproj` is gitignored and never hand-edited.
- `MealPlanner/`: the thin app target. `MealPlannerApp.swift` (`@main`), `Assets.xcassets`. All real logic lives in `MealPlannerKit`.
- `MealPlannerUITests/`: XCUITest target (needs the app target to launch; cannot live inside the SPM package).
- `MealPlannerKit/`: local Swift package, built and tested with `swift build`/`swift test` independent of Xcode.
  - `Sources/API/`: generated OpenAPI client (`GeneratedSources/`, never hand-edited — regenerate with `make generate-ios`) plus `APIClient.swift`, the hand-written client-construction helpers.
  - `Sources/Auth/`: `KeychainTokenStore`, `TokenRefresher` (single-flighted refresh), `BearerAuthMiddleware`, `AuthRepository`, `AuthError`.
  - `Sources/Features/`: one folder per product area (`Auth`, `Today`, `Plan`, `Meals`, `Shopping`, `Profile`) — views and view models. Views never call the network or SwiftData directly (once Repositories/Persistence arrive in the Meals/Shopping plans).
  - `Sources/AppCore/`: `AppState` (`@Observable`, owns `session`), `RootView` (the package's one public entry point — switches sign-in/register vs. the tab shell), `TabShellView`.
  - `Tests/MealPlannerKitTests/`: Swift Testing (`import Testing`, `@Test`/`#expect`). Network-touching code is tested against `StubTransport`, never the real API.

## Commands (from repo root)

- `make build-ios`: `xcodegen generate` then `xcodebuild build` for the simulator. Needs Xcode and XcodeGen (`brew install xcodegen`).
- `make test-ios`: `swift test` on `MealPlannerKit`. No Xcode project, no simulator, no API needed — repositories are tested against `StubTransport`.
- `make generate-ios`: regenerate the Swift OpenAPI client from the root `openapi.yaml`.
- `make check-generated-ios`: fails if the committed generated client differs from what `openapi.yaml` produces now.

## Gotchas

- The generated client uses `namingStrategy: idiomatic` (`Sources/API/openapi-generator-config.yaml`), so schema fields are camelCase in Swift (`displayName`, `accessToken`) even though the JSON wire format stays snake_case. Don't "fix" a camelCase name back to match the YAML — that's the generator working as configured.
- `Sources/API/openapi.yaml` is a symlink to the repo-root spec, not a copy. Never edit it directly; edit the root `openapi.yaml` and run `make generate-ios`.
- `TokenRefresher` single-flights concurrent refreshes: N requests racing a 401 must trigger exactly one `/auth/refresh` call. If you touch `BearerAuthMiddleware` or `TokenRefresher`, `TokenRefresherTests.singleFlight` is the test that catches a regression here — a naive "each request refreshes independently" implementation logs every signed-in user out under load, because the backend revokes a session family when a used refresh token is replayed.
- The login API answers an unknown email and a wrong password identically (`401`, same message) — never show the user which one was wrong; `AuthRepository.login` maps both to one `AuthError.invalidCredentials`.
- `AppState.restoreSession()` calls `GET /me` on launch whenever a refresh token is on disk, rather than trusting a cached "signed in" flag — the API is the source of truth (spec §5). A stale or revoked refresh token surfaces as `.signedOut`, not a crash or a stuck spinner.
- Registering a taken email is `409 email_taken`; a validation failure (e.g. a too-short password) is `400 validation_failed` with a `detail` string. These are different UI messages — don't collapse them into one generic "registration failed."
- Only `Sources/API` and `Sources/Auth` need `import HTTPTypes`/`import OpenAPIRuntime`. `Features` and `AppCore` should never need to import the networking runtime packages directly — if a view needs to inspect a raw HTTP status or header, that logic belongs in `Auth` or a later `Repositories` target, not the view.
```

- [ ] **Step 2: Update the root `CLAUDE.md` repo map**

Modify `CLAUDE.md` — the "Repo map" section currently lists `ios/`: read the file, find the line `- `ios/`: added by own plan`, and replace it with:

```markdown
- `ios/`: SwiftUI iOS app (see `ios/CLAUDE.md`)
```

- [ ] **Step 3: Update the root `CLAUDE.md` commands table**

Modify `CLAUDE.md` — in the "Commands" table, add four rows after the `make generate` row, matching the table's existing style:

```markdown
| `make build-ios` | Build the iOS app for the simulator (needs Xcode + XcodeGen) |
| `make test-ios` | Run the iOS unit tests (no Xcode or simulator needed) |
| `make generate-ios` | Regenerate the iOS Swift OpenAPI client |
| `make check-generated-ios` | Fail if the committed iOS API client is stale |
```

- [ ] **Step 4: Commit**

```bash
git add ios/CLAUDE.md CLAUDE.md
git commit -m "docs: add ios/CLAUDE.md and update root CLAUDE.md for the iOS app"
```

---

## Task 7: CI — unit job, UI job against the real API, and the sign-in XCUITest flow

**Files:**
- Create: `.github/workflows/ios.yml`
- Modify: `ios/MealPlannerUITests/SmokeUITests.swift` (replace with the real sign-in/register/sign-out flow)

**Interfaces:**
- Consumes: `make test-ios`, `make build-ios` (Task 2); the running app's sign-in/register/sign-out UI from Task 5 (accessibility identifiers: `signInEmailField`, `signInPasswordField`, `signInSubmitButton`, `signInErrorMessage`, `showRegisterButton`, `registerDisplayNameField`, `registerEmailField`, `registerPasswordField`, `registerSubmitButton`, `signOutButton`).
- Produces: nothing consumed by later plans directly; this is the last piece of Foundation's own definition of done (spec §1 success criteria).

- [ ] **Step 1: Replace the smoke test with the real auth flow**

Modify `ios/MealPlannerUITests/SmokeUITests.swift` — replace its entire contents:

```swift
import XCTest

final class AuthFlowUITests: XCTestCase {
    override func setUpWithError() throws {
        continueAfterFailure = false
    }

    /// A fresh, unique email per run so repeated CI runs against the same database never collide
    /// on `email_taken` — the same pattern the web app's Playwright suite uses (each test registers
    /// its own user).
    private func uniqueEmail() -> String {
        "ios-ui-\(UUID().uuidString.prefix(8))@example.com"
    }

    func testRegisterThenSignOutThenSignInAgain() throws {
        let app = XCUIApplication()
        app.launch()

        let email = uniqueEmail()

        app.buttons["showRegisterButton"].tap()
        let displayNameField = app.textFields["registerDisplayNameField"]
        XCTAssertTrue(displayNameField.waitForExistence(timeout: 5))
        displayNameField.tap()
        displayNameField.typeText("iOS UI Test")

        let registerEmailField = app.textFields["registerEmailField"]
        registerEmailField.tap()
        registerEmailField.typeText(email)

        let registerPasswordField = app.secureTextFields["registerPasswordField"]
        registerPasswordField.tap()
        registerPasswordField.typeText("correct-horse-battery-staple")

        app.buttons["registerSubmitButton"].tap()

        let signOutButton = app.buttons["signOutButton"]
        XCTAssertTrue(signOutButton.waitForExistence(timeout: 10), "Expected the tab shell (and its sign-out button) after a successful registration")

        signOutButton.tap()

        let signInEmailField = app.textFields["signInEmailField"]
        XCTAssertTrue(signInEmailField.waitForExistence(timeout: 5), "Expected the sign-in screen after signing out")
        signInEmailField.tap()
        signInEmailField.typeText(email)

        let signInPasswordField = app.secureTextFields["signInPasswordField"]
        signInPasswordField.tap()
        signInPasswordField.typeText("correct-horse-battery-staple")

        app.buttons["signInSubmitButton"].tap()

        XCTAssertTrue(app.buttons["signOutButton"].waitForExistence(timeout: 10), "Expected the tab shell again after signing back in")
    }

    func testWrongPasswordShowsGenericError() throws {
        let app = XCUIApplication()
        app.launch()

        let email = uniqueEmail()

        // Register once so the account exists, sign out, then try the wrong password.
        app.buttons["showRegisterButton"].tap()
        let displayNameField = app.textFields["registerDisplayNameField"]
        XCTAssertTrue(displayNameField.waitForExistence(timeout: 5))
        displayNameField.tap()
        displayNameField.typeText("iOS UI Test")
        let registerEmailField = app.textFields["registerEmailField"]
        registerEmailField.tap()
        registerEmailField.typeText(email)
        let registerPasswordField = app.secureTextFields["registerPasswordField"]
        registerPasswordField.tap()
        registerPasswordField.typeText("correct-horse-battery-staple")
        app.buttons["registerSubmitButton"].tap()
        let signOutButton = app.buttons["signOutButton"]
        XCTAssertTrue(signOutButton.waitForExistence(timeout: 10))
        signOutButton.tap()

        let signInEmailField = app.textFields["signInEmailField"]
        XCTAssertTrue(signInEmailField.waitForExistence(timeout: 5))
        signInEmailField.tap()
        signInEmailField.typeText(email)
        let signInPasswordField = app.secureTextFields["signInPasswordField"]
        signInPasswordField.tap()
        signInPasswordField.typeText("definitely-the-wrong-password")
        app.buttons["signInSubmitButton"].tap()

        let errorMessage = app.staticTexts["signInErrorMessage"]
        XCTAssertTrue(errorMessage.waitForExistence(timeout: 10))
        XCTAssertEqual(errorMessage.label, "Invalid email or password.")
    }
}
```

- [ ] **Step 2: Verify the flow locally against a running API**

In one terminal: `make db-up && make migrate && make run-api`.
In another: run the steps from Task 5 Step 13 (build, install, launch) but instead run the UI tests:

```bash
cd ios
DEVICE_UDID=$(xcrun simctl list devices available -j | /usr/bin/python3 -c "
import json, sys
data = json.load(sys.stdin)['devices']
for runtime, devices in data.items():
    if 'iOS' in runtime:
        for d in devices:
            if d['name'].startswith('iPhone'):
                print(d['udid']); raise SystemExit
")
xcodebuild -project MealPlanner.xcodeproj -scheme MealPlanner -destination "id=$DEVICE_UDID" test
```

Expected: `** TEST SUCCEEDED **` for both `testRegisterThenSignOutThenSignInAgain` and `testWrongPasswordShowsGenericError`.

- [ ] **Step 3: Write the CI workflow**

Create `.github/workflows/ios.yml`:

```yaml
name: ios

on:
  pull_request:
    paths:
      - ios/**
      - openapi.yaml
      - Makefile
      - .github/workflows/ios.yml
  push:
    branches: [master]
    paths:
      - ios/**
      - openapi.yaml
      - Makefile
      - .github/workflows/ios.yml

permissions:
  contents: read

concurrency:
  group: ${{ github.workflow }}-${{ github.ref }}
  cancel-in-progress: true

jobs:
  unit:
    runs-on: macos-15
    timeout-minutes: 20
    steps:
      - uses: actions/checkout@v4
      - run: make test-ios
      - run: make check-generated-ios

  ui:
    runs-on: macos-15
    timeout-minutes: 30
    defaults:
      run:
        shell: bash
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: backend/go.mod
          cache-dependency-path: backend/go.sum
      - name: Install and start Postgres
        # macOS runners have no Docker daemon, so service containers (which the backend's own
        # test suite gets via testcontainers) aren't available here — Postgres runs natively.
        run: |
          brew install postgresql@16
          brew link --overwrite --force postgresql@16
          brew services start postgresql@16
          for i in $(seq 1 30); do pg_isready -h 127.0.0.1 -p 5432 && break; sleep 1; done
          createdb -h 127.0.0.1 mealplanner
      - name: Run migrations and start the API
        working-directory: backend
        env:
          DATABASE_URL: postgres://runner@127.0.0.1:5432/mealplanner?sslmode=disable
          JWT_SECRET: dev-only-secret-change-me-0123456789
          ALLOW_DEV_JWT_SECRET: "1"
        run: |
          go run ./cmd/migrate
          go run ./cmd/api &
          for i in $(seq 1 30); do curl -sf http://localhost:8080/readyz && break; sleep 1; done
      - run: brew install xcodegen
      - name: Build and run the iOS UI tests
        working-directory: ios
        run: |
          xcodegen generate
          DEVICE_UDID=$(xcrun simctl list devices available -j | /usr/bin/python3 -c "
          import json, sys
          data = json.load(sys.stdin)['devices']
          for runtime, devices in data.items():
              if 'iOS' in runtime:
                  for d in devices:
                      if d['name'].startswith('iPhone'):
                          print(d['udid']); raise SystemExit
          ")
          xcodebuild -project MealPlanner.xcodeproj -scheme MealPlanner -destination "id=$DEVICE_UDID" test
```

Note: `postgres://runner@...` assumes the GitHub-hosted macOS runner's default user is `runner`, matching its Linux runners. If a self-hosted or differently-provisioned macOS runner is ever used, replace `runner` with `$(whoami)`.

- [ ] **Step 4: Commit**

```bash
git add ios/MealPlannerUITests/SmokeUITests.swift .github/workflows/ios.yml
git commit -m "ios: add the sign-in/register/sign-out XCUITest flow and ios.yml CI"
```

Note: `git mv` was not used because the file's role changed (smoke test → real auth flow) but its path did not; if you prefer the file renamed to `AuthFlowUITests.swift` to match its new class name, do that rename in this same commit with `git mv ios/MealPlannerUITests/SmokeUITests.swift ios/MealPlannerUITests/AuthFlowUITests.swift` before committing.
