import XCTest

final class AuthFlowUITests: AppUITestCase {
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

        // Not about the registration form (the one test above is), so create the account through the API.
        try createAccountViaAPI(email: email, password: "correct-horse-battery-staple", displayName: "iOS UI Test")
        signIn(in: app, email: email, password: "correct-horse-battery-staple")

        XCTAssertTrue(profileTabButton(in: app).waitForExistence(timeout: 45), "Expected the tab shell after signing in")

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

        // The account exists (created through the API); a wrong password must give the generic error.
        try createAccountViaAPI(email: email, password: "correct-horse-battery-staple", displayName: "iOS UI Test")

        let signInEmailField = app.textFields["signInEmailField"]
        waitUntilHittable(signInEmailField, timeout: 5)
        typeVerified(email, field: signInEmailField)
        typeSecret("definitely-the-wrong-password", field: app.secureTextFields["signInPasswordField"])
        app.buttons["signInSubmitButton"].tap()

        let errorMessage = app.staticTexts["signInErrorMessage"]
        XCTAssertTrue(errorMessage.waitForExistence(timeout: 10))
        XCTAssertEqual(errorMessage.label, "Invalid email or password.")
    }
}
