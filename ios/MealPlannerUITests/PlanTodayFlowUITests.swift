import XCTest

final class PlanTodayFlowUITests: AppUITestCase {
    /// Add a meal to Breakfast on Today, change its portion to 2, and see the server's calories double, then find the
    /// meal on Plan. The meal is 100 kcal per serving (one ingredient, 100 g of a 100 kcal/100 g ingredient), so the
    /// ring reads 100 then 200 only if the PUT reached the API and its computed total came back.
    func testAddMealToTodayChangePortionAndSeeItOnPlan() throws {
        let email = uniqueEmail()
        let password = "correct-horse-battery-staple"
        let token = try createAccountViaAPI(email: email, password: password, displayName: "iOS Plan")
        let ingredient = try apiRequest(
            "POST", "/ingredients", token: token,
            json: ["name": "UI Plan Oats", "category": "other", "nutrients": ["calories": 100]], expect: 201
        )
        let meal = try apiRequest("POST", "/meals", token: token, json: ["name": "UI Plan Meal", "servings": 1], expect: 201)
        try apiRequest(
            "PUT", "/meals/\(try XCTUnwrap(meal["id"] as? String))/ingredients", token: token,
            json: ["items": [["ingredient_id": try XCTUnwrap(ingredient["id"] as? String), "quantity": 100, "unit": "g"]]], expect: 200
        )

        let app = XCUIApplication()
        app.launch()
        signIn(in: app, email: email, password: password)

        // Today is the default tab.
        let add = app.buttons["slotAddButton-breakfast"]
        waitUntilHittable(add, timeout: 45)
        add.tap()
        let mealRow = app.buttons["mealPickerRow-UI Plan Meal"]
        waitUntilHittable(mealRow, timeout: 45)
        mealRow.tap()

        let calories = app.descendants(matching: .any).matching(identifier: "ring-calories").firstMatch
        XCTAssertTrue(calories.waitForExistence(timeout: 45))
        expectValue(of: calories, toContain: "100")

        let menu = element(app, identifier: "slotMenu-breakfast", label: "Breakfast options")
        waitUntilHittable(menu, timeout: 45)
        menu.tap()
        let changePortion = element(app, identifier: "changePortionButton", label: "Change portion")
        waitUntilHittable(changePortion)
        changePortion.tap()
        let portionField = app.textFields["portionField"]
        waitUntilHittable(portionField)
        clearAndType("2", field: portionField)
        app.buttons["portionSaveButton"].tap()
        expectValue(of: calories, toContain: "200")

        let planTab = tabButton(in: app, identifier: "planTab", label: "Plan")
        XCTAssertTrue(planTab.waitForExistence(timeout: 45))
        planTab.tap()
        XCTAssertTrue(
            element(withLabelContaining: "UI Plan Meal", in: app).waitForExistence(timeout: 45),
            "Expected today's meal on the Plan tab's week"
        )

        // Leave no session in the Keychain for the next test.
        signOutButton(in: app).tap()
        XCTAssertTrue(app.buttons["showRegisterButton"].waitForExistence(timeout: 45), "Expected the welcome screen after signing out")
    }

    /// A SwiftUI `Menu` and its items are not reliably exposed as `buttons` with their identifier on iOS 26 (CI could
    /// not find `slotMenu-breakfast`), so match any element type by identifier or label, as `tabButton` does.
    private func element(_ app: XCUIApplication, identifier: String, label: String) -> XCUIElement {
        app.descendants(matching: .any)
            .matching(NSPredicate(format: "identifier == %@ OR label == %@", identifier, label))
            .firstMatch
    }
}
