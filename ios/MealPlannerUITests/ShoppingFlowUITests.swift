import XCTest

final class ShoppingFlowUITests: AppUITestCase {
    private func localToday() -> String {
        let formatter = DateFormatter()
        formatter.dateFormat = "yyyy-MM-dd"
        formatter.locale = Locale(identifier: "en_US_POSIX")
        return formatter.string(from: Date())
    }

    /// Taps the debug toggle until its label shows the wanted state. A plain `.tap()` on this toolbar button was
    /// dropped locally ("Computed hit point {-1, -1}"), so it falls back to tapping its centre by coordinate.
    private func tapDebugToggle(_ toggle: XCUIElement, until label: String) {
        for attempt in 0..<4 where toggle.label != label {
            if attempt.isMultiple(of: 2) { toggle.tap() } else { toggle.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.5)).tap() }
            let changed = XCTNSPredicateExpectation(predicate: NSPredicate(format: "label == %@", label), object: toggle)
            _ = XCTWaiter().wait(for: [changed], timeout: 5)
        }
        XCTAssertEqual(toggle.label, label, "Expected the debug toggle to read \"\(label)\"")
    }

    private func itemID(named name: String, inList listID: String, token: String) throws -> String {
        let list = try apiRequest("GET", "/shopping-lists/\(listID)", token: token, expect: 200)
        let items = try XCTUnwrap(list["items"] as? [[String: Any]])
        return try XCTUnwrap(items.first { $0["name"] as? String == name }?["id"] as? String, "no item named \(name)")
    }

    /// A signs in, generates a list from a seeded plan, and B (the partner, over the API) checks an item: it lands on A's
    /// screen without a refresh. A then goes offline, checks another item (it looks bought and a syncing badge shows),
    /// comes back online, and the change reaches the server.
    func testGenerateSeeLiveCheckOffAndSyncAfterOffline() throws {
        let password = "correct-horse-battery-staple"
        let emailA = uniqueEmail()
        let tokenA = try createAccountViaAPI(email: emailA, password: password, displayName: "Shop A")
        let tokenB = try createAccountViaAPI(email: uniqueEmail(), password: password, displayName: "Shop B")

        // Link the two accounts.
        let invite = try apiRequest("POST", "/partner/invite", token: tokenA, expect: 201)
        try apiRequest("POST", "/partner/accept", token: tokenB, json: ["code": try XCTUnwrap(invite["code"] as? String)], expect: 200)

        // Seed A's plan for today with a meal of two ingredients, so the generated list has two items.
        let rice = try apiRequest("POST", "/ingredients", token: tokenA, json: ["name": "UI Shop Rice", "category": "other", "nutrients": ["calories": 130]], expect: 201)
        let beans = try apiRequest("POST", "/ingredients", token: tokenA, json: ["name": "UI Shop Beans", "category": "other", "nutrients": ["calories": 90]], expect: 201)
        let meal = try apiRequest("POST", "/meals", token: tokenA, json: ["name": "UI Shop Meal", "servings": 1], expect: 201)
        let mealID = try XCTUnwrap(meal["id"] as? String)
        try apiRequest(
            "PUT", "/meals/\(mealID)/ingredients", token: tokenA,
            json: ["items": [
                ["ingredient_id": try XCTUnwrap(rice["id"] as? String), "quantity": 100, "unit": "g"],
                ["ingredient_id": try XCTUnwrap(beans["id"] as? String), "quantity": 50, "unit": "g"],
            ]], expect: 200
        )
        try apiRequest("PUT", "/plan/\(localToday())/breakfast", token: tokenA, json: ["meal_id": mealID, "portion": 1], expect: 200)

        let app = XCUIApplication()
        app.launchArguments += ["-uiTesting"]
        app.launch()
        signIn(in: app, email: emailA, password: password)

        // 1. Generate a list from the plan through the UI (the default range starts today).
        let shoppingTab = tabButton(in: app, identifier: "shoppingTab", label: "Shopping")
        XCTAssertTrue(shoppingTab.waitForExistence(timeout: 45))
        let addMenu = app.buttons["addListMenu"]
        // A tab tap right after the shell appears is sometimes lost (the screen stays on Today): tap again.
        for _ in 0..<4 where !addMenu.exists {
            shoppingTab.tap()
            _ = addMenu.waitForExistence(timeout: 10)
        }
        waitUntilHittable(addMenu, timeout: 45)
        addMenu.tap()
        let generate = app.buttons["generateListButton"]
        waitUntilHittable(generate)
        generate.tap()
        let nameField = app.textFields["generateNameField"]
        waitUntilHittable(nameField)
        nameField.tap()
        typeVerified("UI Shop List", field: nameField)
        app.buttons["generateSubmitButton"].tap()

        let riceRow = app.buttons["shoppingItem-UI Shop Rice"]
        let beansRow = app.buttons["shoppingItem-UI Shop Beans"]
        // The sheet dismisses and the list is pushed in the same turn, which can drop the push: if the list did not
        // open, open it from the lists screen.
        if !riceRow.waitForExistence(timeout: 20) {
            let listRow = app.buttons["shoppingListRow-UI Shop List"]
            waitUntilHittable(listRow, timeout: 45)
            listRow.tap()
        }
        waitUntilHittable(riceRow, timeout: 45)
        waitUntilHittable(beansRow, timeout: 45)

        // 2. Share the list and let B check Rice over the API: it must appear on A's screen live.
        let lists = try apiRequest("GET", "/shopping-lists", token: tokenA, expect: 200)
        let listID = try XCTUnwrap((lists["items"] as? [[String: Any]])?.first?["id"] as? String)
        try apiRequest("PATCH", "/shopping-lists/\(listID)", token: tokenA, json: ["shared_with_partner": true], expect: 200)
        let riceID = try itemID(named: "UI Shop Rice", inList: listID, token: tokenB)
        try apiRequest("PATCH", "/shopping-lists/\(listID)/items/\(riceID)", token: tokenB, json: ["checked": true], expect: 200)
        expectValue(of: riceRow, toContain: "bought")

        // 3. Offline: Beans looks bought at once, and the syncing badge shows.
        let offlineToggle = app.buttons["debugOfflineToggle"]
        waitUntilHittable(offlineToggle)
        tapDebugToggle(offlineToggle, until: "Go online") // now offline
        waitUntilHittable(beansRow)
        beansRow.tap()
        expectValue(of: beansRow, toContain: "bought")
        // An icon-only Label in a toolbar group may surface as an image, button or other element: match any type.
        let badge = app.descendants(matching: .any)["syncingBadge"]
        XCTAssertTrue(badge.waitForExistence(timeout: 45), "Expected the syncing badge while offline")

        // 4. Back online: the badge clears and the server has Beans checked.
        tapDebugToggle(offlineToggle, until: "Go offline") // back online
        let badgeGone = NSPredicate(format: "exists == false")
        let gone = XCTNSPredicateExpectation(predicate: badgeGone, object: badge)
        XCTAssertEqual(XCTWaiter().wait(for: [gone], timeout: 45), .completed, "Expected the syncing badge to clear after reconnecting")
        let beansID = try itemID(named: "UI Shop Beans", inList: listID, token: tokenB)
        var beansChecked = false
        for _ in 0..<15 where !beansChecked {
            let list = try apiRequest("GET", "/shopping-lists/\(listID)", token: tokenB, expect: 200)
            let items = try XCTUnwrap(list["items"] as? [[String: Any]])
            beansChecked = items.first { $0["id"] as? String == beansID }?["checked"] as? Bool == true
            if !beansChecked { Thread.sleep(forTimeInterval: 1) }
        }
        XCTAssertTrue(beansChecked, "Expected the offline check-off to reach the server")

        // Leave no session in the Keychain for the next test.
        signOutButton(in: app).tap()
        XCTAssertTrue(app.buttons["showRegisterButton"].waitForExistence(timeout: 45), "Expected the welcome screen after signing out")
    }
}
