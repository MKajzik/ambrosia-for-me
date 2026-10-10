import XCTest

/// Shared helpers for the UI flows. Each exists because of a CI-only failure; the doc comments say why.
class AppUITestCase: XCTestCase {
    override func setUpWithError() throws {
        continueAfterFailure = false
    }

    /// A fresh, unique email per run so repeated CI runs against the same database never collide
    /// on `email_taken` — the same pattern the web app's Playwright suite uses (each test registers
    /// its own user).
    func uniqueEmail() -> String {
        "ios-ui-\(UUID().uuidString.prefix(8))@example.com"
    }

    /// Element existence in the accessibility tree lags behind hit-testability by a beat right
    /// after a full-screen transition (sign-in screen <-> tab shell, or a tab's first appearance):
    /// `waitForExistence` succeeds while the element's hit point still reports `{-1, -1}`, making
    /// an immediate `.tap()` right after the transition flaky. Poll `isHittable` instead.
    func waitUntilHittable(_ element: XCUIElement, timeout: TimeInterval = 10) {
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
    func tabButton(in app: XCUIApplication, identifier: String, label: String) -> XCUIElement {
        app.buttons.matching(NSPredicate(format: "identifier == %@ OR label == %@", identifier, label)).firstMatch
    }

    func profileTabButton(in app: XCUIApplication) -> XCUIElement {
        tabButton(in: app, identifier: "profileTab", label: "Profile")
    }

    /// The tab shell defaults to the Today tab after sign-in/registration; `signOutButton` lives
    /// on the Profile tab, so every flow that needs it must navigate there first.
    func signOutButton(in app: XCUIApplication) -> XCUIElement {
        let profileTab = profileTabButton(in: app)
        XCTAssertTrue(profileTab.waitForExistence(timeout: 45), "Expected the tab shell after a successful sign-in or registration")
        profileTab.tap()
        // Sign Out is the last section of a long Form, so it is not rendered until scrolled near.
        let signOutButton = app.buttons["signOutButton"]
        XCTAssertTrue(scrollUntilExists(signOutButton, in: app), "Expected the Sign Out button on Profile")
        waitUntilHittable(signOutButton)
        return signOutButton
    }

    /// Clears a field before retyping into it — `.typeText` appends at the cursor, so a field with
    /// leftover content from a previous attempt must be cleared first. Always sends a generous
    /// fixed number of deletes rather than computing the field's exact current length (unknowable
    /// for a privacy-masked `SecureField` anyway); extra deletes on an already-empty field are
    /// harmless no-ops.
    func clearAndType(_ text: String, field: XCUIElement) {
        focus(field)
        field.typeText(String(repeating: XCUIKeyboardKey.delete.rawValue, count: 60))
        field.typeText(text)
    }

    /// Types plain text and checks it landed whole before the caller submits. Synthesized keystrokes get dropped on CI
    /// for any field, not only passwords: a dropped character in the email gives "Invalid email or password." for a
    /// correct password. Retype until the field's value equals the text; if the value cannot be read, stop retrying.
    func typeVerified(_ text: String, field: XCUIElement) {
        for attempt in 0..<4 {
            clearAndType(text, field: field)
            guard let value = field.value as? String else { return }
            if value == text { return }
            print("UITEST-TEXT attempt=\(attempt) expected=\(text.count) readCount=\(value.count)")
        }
    }

    /// Finds an element in a scrolling list. A `List` renders rows lazily, so a row below the fold is not in the
    /// accessibility tree until it is scrolled near: the Plan tab shows a whole week, and which day "today" is (so how
    /// far down its row sits) depends on the weekday the test runs on.
    func scrollUntilExists(_ element: XCUIElement, in app: XCUIApplication, maxSwipes: Int = 10) -> Bool {
        if element.waitForExistence(timeout: 15) { return true }
        for _ in 0..<maxSwipes {
            app.swipeUp()
            if element.waitForExistence(timeout: 3) { return true }
        }
        return false
    }

    /// Taps the field and waits for the keyboard: a tap right after a screen transition sometimes leaves no
    /// keyboard focus, and `typeText` then fails the whole test ("Neither element nor any descendant has keyboard
    /// focus") instead of just typing nothing. Tap again once if the keyboard did not appear.
    func focus(_ field: XCUIElement) {
        field.tap()
        let keyboard = XCUIApplication().keyboards.firstMatch
        if !keyboard.waitForExistence(timeout: 5) {
            field.tap()
            _ = keyboard.waitForExistence(timeout: 5)
        }
    }

    /// Types a password into a secure field and checks it landed whole before the caller submits. Password AutoFill
    /// intercepts synthesized keystrokes on CI and sometimes drops characters (CI showed "Password must be at least
    /// 10 characters." and "Invalid email or password." for a correct 29-character password). A secure field's value
    /// is one bullet per character, so its length is the number of characters that arrived: retype until it matches.
    /// If the value cannot be read that way, stop retrying and let the caller's own retry loop cope.
    func typeSecret(_ text: String, field: XCUIElement) {
        for _ in 0..<4 {
            clearAndType(text, field: field)
            guard let value = field.value as? String, value != (field.placeholderValue ?? "") else { return }
            if value.count == text.count { return }
        }
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
    private final class ResponseBox: @unchecked Sendable {
        var code = 0
        var data = Data()
    }

    /// A synchronous JSON request to the API from the test process, for flows that are not about the screens that
    /// would otherwise create the data. Fails the test unless the status is `expect`.
    @discardableResult
    func apiRequest(_ method: String, _ path: String, token: String? = nil, json: [String: Any]? = nil, expect: Int) throws -> [String: Any] {
        var request = URLRequest(url: URL(string: "http://localhost:8080/v1" + path)!)
        request.httpMethod = method
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        if let token { request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization") }
        if let json { request.httpBody = try JSONSerialization.data(withJSONObject: json) }
        let done = expectation(description: "\(method) \(path)")
        let box = ResponseBox()
        URLSession.shared.dataTask(with: request) { data, response, _ in
            box.code = (response as? HTTPURLResponse)?.statusCode ?? 0
            box.data = data ?? Data()
            done.fulfill()
        }.resume()
        wait(for: [done], timeout: 30)
        XCTAssertEqual(box.code, expect, "\(method) \(path)")
        return ((try? JSONSerialization.jsonObject(with: box.data)) as? [String: Any]) ?? [:]
    }

    /// Creates an account straight through the API (the registration form's `.newPassword` field is the one place where
    /// Password AutoFill drops typed characters on CI, so flows that are not about registration sign in instead) and
    /// returns its access token. The Auth flows still register through the form.
    @discardableResult
    func createAccountViaAPI(email: String, password: String, displayName: String) throws -> String {
        let body = try apiRequest("POST", "/auth/register", json: ["email": email, "password": password, "display_name": displayName], expect: 201)
        return try XCTUnwrap(body["access_token"] as? String)
    }

    /// Waits until the element's accessibility value contains `text`.
    func expectValue(of element: XCUIElement, toContain text: String, timeout: TimeInterval = 45, file: StaticString = #filePath, line: UInt = #line) {
        let predicate = NSPredicate(format: "value CONTAINS %@", text)
        let expectation = XCTNSPredicateExpectation(predicate: predicate, object: element)
        XCTAssertEqual(XCTWaiter().wait(for: [expectation], timeout: timeout), .completed, "Expected the value to contain \"\(text)\"", file: file, line: line)
    }

    /// A button or text whose label contains `text` (a plain-styled button's inner text is not a separate element).
    func element(withLabelContaining text: String, in app: XCUIApplication) -> XCUIElement {
        app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", text)).firstMatch
    }

    /// Printed (and so visible in the CI log) when an attempt did not reach the tab shell: the XCUITest log alone
    /// cannot tell a rejected request from a dropped keystroke from a slow server.
    func diagnose(_ app: XCUIApplication, _ what: String, errorID: String, submitID: String) {
        let error = app.staticTexts[errorID]
        let texts = app.staticTexts.allElementsBoundByIndex.map { $0.label }
        print("UITEST-DIAG \(what) did not reach the shell: error=\(error.exists ? error.label : "none") submitEnabled=\(app.buttons[submitID].isEnabled) texts=\(texts)")
    }

    // CI runners vary a lot: the transition to the tab shell took ~3 s on a fast run and 9 s or more on a slow
    // one (register/sign-in hash a password server-side, on the same machine as the simulator). A short wait
    // here misreads a slow success as a failure; the retry then drives a form that is already gone and the
    // test dies with a signed-in session left in the Keychain, which breaks every later test. So: wait 20 s
    // per attempt, and never retry once the shell is up.
    func registerAccount(in app: XCUIApplication, displayName: String, email: String, password: String) {
        for attempt in 0..<5 {
            if profileTabButton(in: app).exists { return }
            let displayNameField = app.textFields["registerDisplayNameField"]
            waitUntilHittable(displayNameField, timeout: 5)
            typeVerified(displayName, field: displayNameField)
            typeVerified(email, field: app.textFields["registerEmailField"])
            typeSecret(password, field: app.secureTextFields["registerPasswordField"])
            app.buttons["registerSubmitButton"].tap()
            if profileTabButton(in: app).waitForExistence(timeout: 20) {
                return
            }
            diagnose(app, "register attempt \(attempt)", errorID: "registerErrorMessage", submitID: "registerSubmitButton")
        }
    }

    /// Signs in with the given credentials, retrying the whole form fill if it doesn't reach the
    /// tab shell — only used where success is the sole valid outcome (a just-registered account
    /// signing back in with its real password), for the same AutoFill-truncation reason
    /// `registerAccount` retries. Never used for a deliberately wrong password: there, a sign-in
    /// error is the expected, correct result, not a signal to retry.
    func signIn(in app: XCUIApplication, email: String, password: String) {
        for attempt in 0..<5 {
            if profileTabButton(in: app).exists { return }
            let emailField = app.textFields["signInEmailField"]
            waitUntilHittable(emailField, timeout: 5)
            typeVerified(email, field: emailField)
            typeSecret(password, field: app.secureTextFields["signInPasswordField"])
            app.buttons["signInSubmitButton"].tap()
            if profileTabButton(in: app).waitForExistence(timeout: 20) {
                return
            }
            diagnose(app, "sign-in attempt \(attempt)", errorID: "signInErrorMessage", submitID: "signInSubmitButton")
        }
    }
}
