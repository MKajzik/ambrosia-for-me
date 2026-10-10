import XCTest

final class ProfileFlowUITests: AppUITestCase {
    private func localToday() -> String {
        let formatter = DateFormatter()
        formatter.dateFormat = "yyyy-MM-dd"
        formatter.locale = Locale(identifier: "en_US_POSIX")
        return formatter.string(from: Date())
    }

    /// Taps `element` once it sits in the clear middle of the screen. "Hittable" is not enough: a full-screen swipe can
    /// leave a button under the status and navigation bars (or behind the keyboard), where a tap lands on the bar and
    /// never reaches it. So the form is nudged with short drags until the element is clear of both.
    private func tapWhenHittable(_ element: XCUIElement, in app: XCUIApplication) {
        XCTAssertTrue(element.waitForExistence(timeout: 45), "Expected element to exist: \(element)")
        let screen = app.windows.firstMatch.frame
        func drag(by points: CGFloat) {
            let from = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.5))
            let to = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.5 + points / screen.height))
            from.press(forDuration: 0.1, thenDragTo: to)
        }
        for _ in 0..<8 {
            let frame = element.frame
            let top = screen.minY + 140
            let bottom = screen.maxY - (app.keyboards.count > 0 ? 360 : 120)
            if frame.minY < top {
                drag(by: min(top - frame.minY + 20, 300))
            } else if frame.maxY > bottom {
                drag(by: -min(frame.maxY - bottom + 20, 300))
            } else {
                break
            }
        }
        waitUntilHittable(element, timeout: 45)
        element.tap()
    }

    private func openProfile(in app: XCUIApplication) {
        let profileTab = profileTabButton(in: app)
        XCTAssertTrue(profileTab.waitForExistence(timeout: 45))
        // A tab tap right after the shell appears is sometimes lost: tap again until the screen is up.
        let email = app.staticTexts["profileEmail"]
        for _ in 0..<4 where !email.exists {
            profileTab.tap()
            _ = email.waitForExistence(timeout: 10)
        }
    }

    /// Profile end to end. A signs in with a plan for today that has a 100 kcal meal; sets a calorie target of 2000 and
    /// sees it on Today's ring; enters the invite code of a second account (made over the API) and sees the link; creates,
    /// renames and deletes a custom ingredient; deletes the account by typing the email, and lands on the welcome screen.
    func testTargetsPartnerIngredientsAndDeletingTheAccount() throws {
        let password = "correct-horse-battery-staple"
        let emailA = uniqueEmail()
        let tokenA = try createAccountViaAPI(email: emailA, password: password, displayName: "Profile A")
        let tokenB = try createAccountViaAPI(email: uniqueEmail(), password: password, displayName: "Profile B")
        let invite = try apiRequest("POST", "/partner/invite", token: tokenB, expect: 201)
        let code = try XCTUnwrap(invite["code"] as? String)

        // A plan for today, so the Today ring has an amount to put against the target.
        let ingredient = try apiRequest(
            "POST", "/ingredients", token: tokenA,
            json: ["name": "UI Profile Oats", "category": "other", "nutrients": ["calories": 100]], expect: 201
        )
        let meal = try apiRequest("POST", "/meals", token: tokenA, json: ["name": "UI Profile Meal", "servings": 1], expect: 201)
        let mealID = try XCTUnwrap(meal["id"] as? String)
        try apiRequest(
            "PUT", "/meals/\(mealID)/ingredients", token: tokenA,
            json: ["items": [["ingredient_id": try XCTUnwrap(ingredient["id"] as? String), "quantity": 100, "unit": "g"]]], expect: 200
        )
        try apiRequest("PUT", "/plan/\(localToday())/breakfast", token: tokenA, json: ["meal_id": mealID, "portion": 1], expect: 200)

        let app = XCUIApplication()
        app.launch()
        // Whatever fails below, never leave A signed in for the next test. Tolerant: with no tab shell (already signed
        // out, deleted, or sign-in never finished) there is nothing to do.
        addTeardownBlock {
            let profileTab = app.buttons.matching(NSPredicate(format: "identifier == %@ OR label == %@", "profileTab", "Profile")).firstMatch
            guard app.state == .runningForeground, profileTab.waitForExistence(timeout: 5) else { return }
            profileTab.tap()
            let signOut = app.buttons["signOutButton"]
            // Sign Out is at the bottom of a long Form and not rendered until scrolled near.
            for _ in 0..<6 where !signOut.waitForExistence(timeout: 3) { app.swipeUp() }
            if signOut.exists { signOut.tap() }
        }
        signIn(in: app, email: emailA, password: password)

        // 1. A calorie target of 2000, then Today's ring shows it.
        openProfile(in: app)
        let calories = app.textFields["targetCaloriesField"]
        XCTAssertTrue(scrollUntilExists(calories, in: app))
        waitUntilHittable(calories, timeout: 45)
        calories.tap()
        typeVerified("2000", field: calories)
        let save = app.buttons["targetsSaveButton"]
        let banner = app.staticTexts["targetsBanner"]
        tapWhenHittable(save, in: app)
        // Save is disabled again once the saved targets are adopted. Switching tabs before that lets Today read the old
        // targets and nothing re-reads them. A refused save keeps the button enabled and shows a banner: say what it said.
        func saved(within seconds: TimeInterval) -> Bool {
            let deadline = Date().addingTimeInterval(seconds)
            while Date() < deadline {
                if !save.isEnabled || banner.exists { return !banner.exists }
                Thread.sleep(forTimeInterval: 0.5)
            }
            return false
        }
        if !saved(within: 15), !banner.exists {
            tapWhenHittable(save, in: app) // a tap right after typing is sometimes lost; saving twice is harmless
        }
        XCTAssertTrue(saved(within: 45), "Expected the targets to be saved. Banner: \(banner.exists ? banner.label : "none")")

        // The calories field still has keyboard focus, and the keyboard covers the tab bar. The decimal pad has no Return key.
        let done = app.buttons["keyboardDoneButton"]
        if done.waitForExistence(timeout: 5) {
            done.tap()
            _ = app.keyboards.firstMatch.waitForNonExistence(timeout: 10)
        }

        let todayTab = tabButton(in: app, identifier: "todayTab", label: "Today")
        waitUntilHittable(todayTab)
        todayTab.tap()
        let ring = app.descendants(matching: .any).matching(identifier: "ring-calories").firstMatch
        XCTAssertTrue(ring.waitForExistence(timeout: 45))
        expectValue(of: ring, toContain: "2,000")

        // 2. Link with the second account by its invite code.
        openProfile(in: app)
        let codeField = app.textFields["partnerCodeField"]
        XCTAssertTrue(scrollUntilExists(codeField, in: app))
        waitUntilHittable(codeField, timeout: 45)
        codeField.tap()
        typeVerified(code, field: codeField)
        tapWhenHittable(app.buttons["partnerLinkButton"], in: app)
        let linked = app.staticTexts["partnerLinkedLabel"]
        let isLinked = linked.waitForExistence(timeout: 45)
        if !isLinked { print("PROFILE-FLOW-DEBUG link tree:\n\(app.debugDescription)") }
        let acceptError = app.staticTexts["partnerAcceptError"]
        let alert = app.alerts.firstMatch
        XCTAssertTrue(
            isLinked,
            "Expected the linked state after entering the code. Inline error: \(acceptError.exists ? acceptError.label : "none"). "
                + "Alert: \(alert.exists ? alert.label : "none")"
        )
        XCTAssertTrue(linked.label.contains("Profile B"), "Expected the partner's name, got \(linked.label)")

        // 3. A custom ingredient: create, rename, delete.
        let ingredientsRow = app.descendants(matching: .any)["myIngredientsRow"]
        XCTAssertTrue(scrollUntilExists(ingredientsRow, in: app))
        waitUntilHittable(ingredientsRow, timeout: 45)
        ingredientsRow.tap()

        let newButton = app.buttons["myIngredientsNewButton"]
        waitUntilHittable(newButton, timeout: 45)
        newButton.tap()
        let nameField = app.textFields["customIngredientNameField"]
        waitUntilHittable(nameField, timeout: 45)
        nameField.tap()
        typeVerified("UI Profile Jam", field: nameField)
        tapWhenHittable(app.buttons["customIngredientSaveButton"], in: app)
        XCTAssertTrue(app.staticTexts["myIngredientRow-UI Profile Jam"].waitForExistence(timeout: 45), "Expected the new ingredient in the list")

        let menu = app.buttons["ingredientMenu-UI Profile Jam"]
        waitUntilHittable(menu)
        menu.tap()
        let edit = app.buttons["ingredientEditButton"]
        waitUntilHittable(edit)
        edit.tap()
        let renameField = app.textFields["customIngredientNameField"]
        waitUntilHittable(renameField, timeout: 45)
        clearAndType("UI Profile Jam 2", field: renameField)
        tapWhenHittable(app.buttons["customIngredientSaveButton"], in: app)
        XCTAssertTrue(app.staticTexts["myIngredientRow-UI Profile Jam 2"].waitForExistence(timeout: 45), "Expected the renamed ingredient")

        let renamedMenu = app.buttons["ingredientMenu-UI Profile Jam 2"]
        waitUntilHittable(renamedMenu)
        renamedMenu.tap()
        let delete = app.buttons["ingredientDeleteButton"]
        waitUntilHittable(delete)
        delete.tap()
        let confirmDelete = app.buttons["Delete ingredient"]
        waitUntilHittable(confirmDelete)
        confirmDelete.tap()
        let gone = XCTNSPredicateExpectation(predicate: NSPredicate(format: "exists == false"), object: app.staticTexts["myIngredientRow-UI Profile Jam 2"])
        XCTAssertEqual(XCTWaiter().wait(for: [gone], timeout: 45), .completed, "Expected the ingredient to disappear after deleting it")

        // 4. Delete the account: the email is typed, and the welcome screen follows.
        app.navigationBars.buttons["Profile"].tap() // back to Profile
        let deleteAccount = app.buttons["deleteAccountButton"]
        XCTAssertTrue(scrollUntilExists(deleteAccount, in: app))
        waitUntilHittable(deleteAccount, timeout: 45)
        deleteAccount.tap()
        let emailField = app.textFields["deleteAccountEmailField"]
        waitUntilHittable(emailField, timeout: 45)
        emailField.tap()
        typeVerified(emailA, field: emailField)
        tapWhenHittable(app.buttons["deleteAccountConfirmButton"], in: app)
        XCTAssertTrue(app.buttons["showRegisterButton"].waitForExistence(timeout: 45), "Expected the welcome screen after deleting the account")

        // The account is really gone on the server: its credentials no longer work.
        try apiRequest("POST", "/auth/login", json: ["email": emailA, "password": password], expect: 401)
    }
}
