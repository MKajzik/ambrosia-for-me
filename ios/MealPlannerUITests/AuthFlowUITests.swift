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

    /// Element existence in the accessibility tree lags behind hit-testability by a beat right
    /// after a full-screen transition (sign-in screen <-> tab shell, or a tab's first appearance):
    /// `waitForExistence` succeeds while the element's hit point still reports `{-1, -1}`, making
    /// an immediate `.tap()` right after the transition flaky. Poll `isHittable` instead.
    private func waitUntilHittable(_ element: XCUIElement, timeout: TimeInterval = 10) {
        XCTAssertTrue(element.waitForExistence(timeout: timeout), "Expected element to exist: \(element)")
        let deadline = Date().addingTimeInterval(timeout)
        while !element.isHittable && Date() < deadline {
            Thread.sleep(forTimeInterval: 0.1)
        }
    }

    /// XCUITest's accessibility snapshot sometimes never re-attaches `profileTab`'s identifier
    /// after any app launch past the first one in a given `xcodebuild test` invocation — whether
    /// that's `app.terminate()` + `.launch()` within one test, or simply a later test method's own
    /// fresh `XCUIApplication().launch()` — even though the app's own session restore is provably
    /// correct: instrumenting the app with `os.Logger` during this investigation showed SwiftUI
    /// rebuilding the signed-in tab shell within ~1s of the launch, every time. The accessibility
    /// label, unlike the identifier, was reliably present in the same snapshot, so match on either.
    private func profileTabButton(in app: XCUIApplication) -> XCUIElement {
        app.buttons.matching(NSPredicate(format: "identifier == %@ OR label == %@", "profileTab", "Profile")).firstMatch
    }

    /// The tab shell defaults to the Today tab after sign-in/registration; `signOutButton` lives
    /// on the Profile tab, so every flow that needs it must navigate there first.
    private func signOutButton(in app: XCUIApplication) -> XCUIElement {
        let profileTab = profileTabButton(in: app)
        XCTAssertTrue(profileTab.waitForExistence(timeout: 45), "Expected the tab shell after a successful sign-in or registration")
        profileTab.tap()
        let signOutButton = app.buttons["signOutButton"]
        waitUntilHittable(signOutButton)
        return signOutButton
    }

    /// Clears a field before retyping into it — `.typeText` appends at the cursor, so a field with
    /// leftover content from a previous attempt must be cleared first. Always sends a generous
    /// fixed number of deletes rather than computing the field's exact current length (unknowable
    /// for a privacy-masked `SecureField` anyway); extra deletes on an already-empty field are
    /// harmless no-ops.
    private func clearAndType(_ text: String, field: XCUIElement) {
        field.tap()
        field.typeText(String(repeating: XCUIKeyboardKey.delete.rawValue, count: 60))
        field.typeText(text)
    }

    /// Registers a new account, retrying the whole form fill if it doesn't reach the tab shell.
    ///
    /// Password entry here went through two failed approaches before this one. `typeText`
    /// directly into the `SecureField` is unreliable once the app carries a real code signature:
    /// Password AutoFill intercepts XCUITest's synthesized keystrokes and sometimes drops
    /// characters, occasionally leaving the password too short
    /// (see `.superpowers/sdd/2026-09-30-ios-foundation/progress.md`). Writing to the pasteboard
    /// and sending a hardware-keyboard Cmd+V shortcut (`field.typeKey("v", modifierFlags: .command)`)
    /// was tried next specifically to sidestep that — but it turned out to deterministically
    /// deliver *zero* characters whenever the app under test is not the very first one launched in
    /// a given `xcodebuild test` invocation: confirmed live via a CI diagnostic that read
    /// `registerPasswordField.value` back as its placeholder text (not even a masked `"•"`) after
    /// 5 retries with a settle delay, every single run, while the exact same `typeKey` call always
    /// succeeded in the first test method's app launch. That's a distinct, apparently permanent
    /// XCUITest limitation, unrelated to AutoFill, that no amount of retrying or waiting works
    /// around.
    ///
    /// `typeText` has never once failed on the display name or email fields, on any launch — it's
    /// the one mechanism proven reliable across every launch — so it's used here for the password
    /// too, with retries to absorb its own, separate AutoFill-truncation risk: a client-side-
    /// disabled submit button never reaches the network, and a server-rejected attempt (password
    /// too short) doesn't register a duplicate account, so retrying from a cleared form is safe.
    private func registerAccount(in app: XCUIApplication, displayName: String, email: String, password: String) {
        for _ in 0..<5 {
            let displayNameField = app.textFields["registerDisplayNameField"]
            waitUntilHittable(displayNameField, timeout: 5)
            clearAndType(displayName, field: displayNameField)
            clearAndType(email, field: app.textFields["registerEmailField"])
            clearAndType(password, field: app.secureTextFields["registerPasswordField"])
            app.buttons["registerSubmitButton"].tap()
            if profileTabButton(in: app).waitForExistence(timeout: 8) {
                return
            }
        }
    }

    /// Signs in with the given credentials, retrying the whole form fill if it doesn't reach the
    /// tab shell — only used where success is the sole valid outcome (a just-registered account
    /// signing back in with its real password), for the same AutoFill-truncation reason
    /// `registerAccount` retries. Never used for a deliberately wrong password: there, a sign-in
    /// error is the expected, correct result, not a signal to retry.
    private func signIn(in app: XCUIApplication, email: String, password: String) {
        for _ in 0..<5 {
            let emailField = app.textFields["signInEmailField"]
            waitUntilHittable(emailField, timeout: 5)
            clearAndType(email, field: emailField)
            clearAndType(password, field: app.secureTextFields["signInPasswordField"])
            app.buttons["signInSubmitButton"].tap()
            if profileTabButton(in: app).waitForExistence(timeout: 8) {
                return
            }
        }
    }

    func testRegisterThenSignOutThenSignInAgain() throws {
        let app = XCUIApplication()
        app.launch()

        let email = uniqueEmail()

        app.buttons["showRegisterButton"].tap()
        registerAccount(in: app, displayName: "iOS UI Test", email: email, password: "correct-horse-battery-staple")

        let firstSignOutButton = signOutButton(in: app)
        firstSignOutButton.tap()

        waitUntilHittable(app.textFields["signInEmailField"], timeout: 5)
        signIn(in: app, email: email, password: "correct-horse-battery-staple")

        // Sign out at the end so this test leaves no session in the Keychain: tests run against
        // the same simulator and a dangling signed-in session makes the next test (which expects
        // the signed-out welcome screen on launch) fail before it even starts. `onSignOut` is a
        // fire-and-forget `Task`, so wait for the welcome screen to actually reappear — otherwise
        // the test can end (and the next one launch) before the Keychain is actually cleared.
        signOutButton(in: app).tap()
        XCTAssertTrue(app.buttons["showRegisterButton"].waitForExistence(timeout: 45), "Expected the welcome screen after signing out")
    }

    /// Pins the Critical finding from this plan's final review: a build without a real code
    /// signature (`CODE_SIGNING_ALLOWED: NO`) produces a binary with no `application-identifier`
    /// entitlement, so every `SecItemAdd`/`SecItemCopyMatching` silently fails and the Keychain
    /// never actually persists a session — confirmed live by watching this exact test fail before
    /// `project.yml` was fixed to allow a normal ad-hoc simulator signature.
    func testSessionPersistsAcrossRelaunch() throws {
        let app = XCUIApplication()
        app.launch()

        let email = uniqueEmail()

        app.buttons["showRegisterButton"].tap()
        registerAccount(in: app, displayName: "iOS UI Test", email: email, password: "correct-horse-battery-staple")

        XCTAssertTrue(profileTabButton(in: app).waitForExistence(timeout: 45), "Expected the tab shell after registration")

        app.terminate()
        app.launch()

        XCTAssertTrue(
            profileTabButton(in: app).waitForExistence(timeout: 45),
            "Expected the tab shell to reappear after relaunch — the Keychain-stored session should restore without signing in again"
        )

        // Sign out at the end so this test leaves no session in the Keychain for the next test —
        // see the comment at the end of testRegisterThenSignOutThenSignInAgain.
        signOutButton(in: app).tap()
        XCTAssertTrue(app.buttons["showRegisterButton"].waitForExistence(timeout: 45), "Expected the welcome screen after signing out")
    }

    func testWrongPasswordShowsGenericError() throws {
        let app = XCUIApplication()
        app.launch()

        let email = uniqueEmail()

        // Register once so the account exists, sign out, then try the wrong password.
        app.buttons["showRegisterButton"].tap()
        registerAccount(in: app, displayName: "iOS UI Test", email: email, password: "correct-horse-battery-staple")

        let firstSignOutButton = signOutButton(in: app)
        firstSignOutButton.tap()

        let signInEmailField = app.textFields["signInEmailField"]
        waitUntilHittable(signInEmailField, timeout: 5)
        clearAndType(email, field: signInEmailField)
        clearAndType("definitely-the-wrong-password", field: app.secureTextFields["signInPasswordField"])
        app.buttons["signInSubmitButton"].tap()

        let errorMessage = app.staticTexts["signInErrorMessage"]
        XCTAssertTrue(errorMessage.waitForExistence(timeout: 10))
        XCTAssertEqual(errorMessage.label, "Invalid email or password.")
    }
}
