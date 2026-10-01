import UIKit
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
    /// that's `app.terminate()` + `.launch()` within one test, or simply a later `@Test`/test
    /// method's own fresh `XCUIApplication().launch()` — even though the app's own session
    /// restore is provably correct: instrumenting the app with `os.Logger` during this
    /// investigation showed SwiftUI rebuilding the signed-in tab shell within ~1s of the launch,
    /// every time. The accessibility label, unlike the identifier, was reliably present in the
    /// same snapshot, so match on either.
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

    /// A `SecureField`'s accessibility `value` is privacy-masked to a fixed `"•"` regardless of
    /// how many characters were actually entered, so a test can never verify the *exact* text
    /// landed — but an empty field reports no `value` at all (confirmed by inspecting a live CI
    /// failure's accessibility snapshot: an empty `SecureField` shows no `value:` line, a
    /// non-empty one shows `value: •`), which is enough to detect the specific failure mode seen
    /// here: a *complete* paste failure, not truncation.
    ///
    /// `typeText` into a `SecureField` is unreliable once the app carries a real code signature:
    /// Password AutoFill intercepts XCUITest's synthesized keystrokes and the password sometimes
    /// arrives truncated (see `.superpowers/sdd/2026-09-30-ios-foundation/progress.md`). Writing
    /// to the pasteboard and sending the hardware-keyboard Cmd+V shortcut sidesteps AutoFill and
    /// fixes the truncation, but the paste itself is sometimes a complete no-op (confirmed live:
    /// a CI failure's accessibility snapshot showed the password field completely empty and the
    /// submit button still disabled, right after this exact paste sequence ran) — so verify it
    /// landed and retry a few times before giving up.
    private func typeIntoSecureField(_ text: String, field: XCUIElement) {
        UIPasteboard.general.string = text
        for _ in 0..<5 {
            field.tap()
            field.typeKey("v", modifierFlags: .command)
            if let value = field.value as? String, !value.isEmpty {
                return
            }
            Thread.sleep(forTimeInterval: 0.3)
        }
    }

    func testRegisterThenSignOutThenSignInAgain() throws {
        let app = XCUIApplication()
        app.launch()

        let email = uniqueEmail()

        app.buttons["showRegisterButton"].tap()
        let displayNameField = app.textFields["registerDisplayNameField"]
        waitUntilHittable(displayNameField, timeout: 5)
        displayNameField.tap()
        displayNameField.typeText("iOS UI Test")

        let registerEmailField = app.textFields["registerEmailField"]
        registerEmailField.tap()
        registerEmailField.typeText(email)

        let registerPasswordField = app.secureTextFields["registerPasswordField"]
        typeIntoSecureField("correct-horse-battery-staple", field: registerPasswordField)

        app.buttons["registerSubmitButton"].tap()

        let firstSignOutButton = signOutButton(in: app)
        firstSignOutButton.tap()

        let signInEmailField = app.textFields["signInEmailField"]
        waitUntilHittable(signInEmailField, timeout: 5)
        signInEmailField.tap()
        signInEmailField.typeText(email)

        let signInPasswordField = app.secureTextFields["signInPasswordField"]
        typeIntoSecureField("correct-horse-battery-staple", field: signInPasswordField)

        app.buttons["signInSubmitButton"].tap()

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
        let displayNameField = app.textFields["registerDisplayNameField"]
        waitUntilHittable(displayNameField, timeout: 5)
        displayNameField.tap()
        displayNameField.typeText("iOS UI Test")
        let registerEmailField = app.textFields["registerEmailField"]
        registerEmailField.tap()
        registerEmailField.typeText(email)
        let registerPasswordField = app.secureTextFields["registerPasswordField"]
        typeIntoSecureField("correct-horse-battery-staple", field: registerPasswordField)
        app.buttons["registerSubmitButton"].tap()

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
        let displayNameField = app.textFields["registerDisplayNameField"]
        waitUntilHittable(displayNameField, timeout: 5)
        displayNameField.tap()
        displayNameField.typeText("iOS UI Test")
        let registerEmailField = app.textFields["registerEmailField"]
        registerEmailField.tap()
        registerEmailField.typeText(email)
        let registerPasswordField = app.secureTextFields["registerPasswordField"]
        typeIntoSecureField("correct-horse-battery-staple", field: registerPasswordField)
        app.buttons["registerSubmitButton"].tap()

        let firstSignOutButton = signOutButton(in: app)
        firstSignOutButton.tap()

        let signInEmailField = app.textFields["signInEmailField"]
        waitUntilHittable(signInEmailField, timeout: 5)
        signInEmailField.tap()
        signInEmailField.typeText(email)
        let signInPasswordField = app.secureTextFields["signInPasswordField"]
        typeIntoSecureField("definitely-the-wrong-password", field: signInPasswordField)
        app.buttons["signInSubmitButton"].tap()

        let errorMessage = app.staticTexts["signInErrorMessage"]
        XCTAssertTrue(errorMessage.waitForExistence(timeout: 10))
        XCTAssertEqual(errorMessage.label, "Invalid email or password.")
    }
}
