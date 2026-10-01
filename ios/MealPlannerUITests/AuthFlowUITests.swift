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

    /// The tab shell defaults to the Today tab after sign-in/registration; `signOutButton` lives
    /// on the Profile tab, so every flow that needs it must navigate there first.
    private func signOutButton(in app: XCUIApplication) -> XCUIElement {
        let profileTab = app.buttons["profileTab"]
        XCTAssertTrue(profileTab.waitForExistence(timeout: 10), "Expected the tab shell after a successful sign-in or registration")
        profileTab.tap()
        return app.buttons["signOutButton"]
    }

    /// A `SecureField`'s accessibility `value` is privacy-masked to a fixed placeholder (confirmed
    /// live: it reads `"•"` whether 3 or 8 characters were actually typed), so a test can never
    /// verify how much text landed by reading it back.
    private func typeIntoSecureField(_ text: String, field: XCUIElement) {
        field.tap()
        Thread.sleep(forTimeInterval: 0.5)
        field.typeText(text)
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
        typeIntoSecureField("correct-horse-battery-staple", field: registerPasswordField)

        app.buttons["registerSubmitButton"].tap()

        let firstSignOutButton = signOutButton(in: app)
        XCTAssertTrue(firstSignOutButton.waitForExistence(timeout: 10), "Expected the sign-out button on the Profile tab after a successful registration")
        firstSignOutButton.tap()

        let signInEmailField = app.textFields["signInEmailField"]
        XCTAssertTrue(signInEmailField.waitForExistence(timeout: 5), "Expected the sign-in screen after signing out")
        signInEmailField.tap()
        signInEmailField.typeText(email)

        let signInPasswordField = app.secureTextFields["signInPasswordField"]
        typeIntoSecureField("correct-horse-battery-staple", field: signInPasswordField)

        app.buttons["signInSubmitButton"].tap()

        let secondSignOutButton = signOutButton(in: app)
        XCTAssertTrue(secondSignOutButton.waitForExistence(timeout: 10), "Expected the sign-out button on the Profile tab again after signing back in")
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
        XCTAssertTrue(displayNameField.waitForExistence(timeout: 5))
        displayNameField.tap()
        displayNameField.typeText("iOS UI Test")
        let registerEmailField = app.textFields["registerEmailField"]
        registerEmailField.tap()
        registerEmailField.typeText(email)
        let registerPasswordField = app.secureTextFields["registerPasswordField"]
        typeIntoSecureField("correct-horse-battery-staple", field: registerPasswordField)
        app.buttons["registerSubmitButton"].tap()

        XCTAssertTrue(app.buttons["profileTab"].waitForExistence(timeout: 10), "Expected the tab shell after registration")

        app.terminate()
        app.launch()

        XCTAssertTrue(
            app.buttons["profileTab"].waitForExistence(timeout: 10),
            "Expected the tab shell to reappear after relaunch — the Keychain-stored session should restore without signing in again"
        )
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
        typeIntoSecureField("correct-horse-battery-staple", field: registerPasswordField)
        app.buttons["registerSubmitButton"].tap()

        let firstSignOutButton = signOutButton(in: app)
        XCTAssertTrue(firstSignOutButton.waitForExistence(timeout: 10))
        firstSignOutButton.tap()

        let signInEmailField = app.textFields["signInEmailField"]
        XCTAssertTrue(signInEmailField.waitForExistence(timeout: 5))
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
