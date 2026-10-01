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

        let firstSignOutButton = signOutButton(in: app)
        XCTAssertTrue(firstSignOutButton.waitForExistence(timeout: 10), "Expected the sign-out button on the Profile tab after a successful registration")
        firstSignOutButton.tap()

        let signInEmailField = app.textFields["signInEmailField"]
        XCTAssertTrue(signInEmailField.waitForExistence(timeout: 5), "Expected the sign-in screen after signing out")
        signInEmailField.tap()
        signInEmailField.typeText(email)

        let signInPasswordField = app.secureTextFields["signInPasswordField"]
        signInPasswordField.tap()
        signInPasswordField.typeText("correct-horse-battery-staple")

        app.buttons["signInSubmitButton"].tap()

        let secondSignOutButton = signOutButton(in: app)
        XCTAssertTrue(secondSignOutButton.waitForExistence(timeout: 10), "Expected the sign-out button on the Profile tab again after signing back in")
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

        let firstSignOutButton = signOutButton(in: app)
        XCTAssertTrue(firstSignOutButton.waitForExistence(timeout: 10))
        firstSignOutButton.tap()

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
