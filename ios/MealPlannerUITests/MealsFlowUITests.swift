import XCTest

final class MealsFlowUITests: AppUITestCase {
    /// Sign in to an API-created account, open Meals, create a meal, add a custom ingredient with calories, and see the server's nutrition
    /// for it. Seeing "52" in the Calories tile proves the autosave `PUT` reached the API and the API's
    /// computed nutrition came back (a 100 g row of a 52 kcal/100 g ingredient, one serving).
    func testCreateMealWithCustomIngredientAutosaves() throws {
        let app = XCUIApplication()
        app.launch()

        let email = uniqueEmail()
        let password = "correct-horse-battery-staple"
        try createAccountViaAPI(email: email, password: password, displayName: "iOS Meals")
        signIn(in: app, email: email, password: password)

        let mealsTab = tabButton(in: app, identifier: "mealsTab", label: "Meals")
        XCTAssertTrue(mealsTab.waitForExistence(timeout: 45), "Expected the tab shell after signing in")
        mealsTab.tap()

        let newMeal = app.buttons["newMealButton"]
        waitUntilHittable(newMeal, timeout: 45)
        newMeal.tap()

        let nameField = app.textFields["newMealNameField"]
        waitUntilHittable(nameField)
        nameField.tap()
        nameField.typeText("UI Test Porridge")
        app.buttons["newMealCreateButton"].tap()

        // The same sheet becomes the editor.
        let addIngredient = app.buttons["addIngredientButton"]
        waitUntilHittable(addIngredient, timeout: 45)
        addIngredient.tap()

        let searchField = app.textFields["ingredientSearchField"]
        waitUntilHittable(searchField)
        searchField.tap()
        searchField.typeText("UI Oats")

        let createCustom = app.buttons["createCustomIngredientButton"]
        waitUntilHittable(createCustom)
        createCustom.tap()

        let customName = app.textFields["customIngredientNameField"]
        waitUntilHittable(customName)
        XCTAssertEqual(customName.value as? String, "UI Oats", "The search text should prefill the name")
        let calories = app.textFields["customIngredientCaloriesField"]
        waitUntilHittable(calories)
        calories.tap()
        calories.typeText("52")
        let save = app.buttons["customIngredientSaveButton"]
        waitUntilHittable(save)
        save.tap()

        // Both sheets close and the ingredient is a row of the meal.
        XCTAssertTrue(app.staticTexts["UI Oats"].waitForExistence(timeout: 45), "Expected the new ingredient as a row")

        // The server's answer to the autosave: 100 g of 52 kcal/100 g, one serving.
        let caloriesTile = app.descendants(matching: .any).matching(identifier: "nutrientTile-calories").firstMatch
        XCTAssertTrue(caloriesTile.waitForExistence(timeout: 45))
        let tileShowsCalories = XCTNSPredicateExpectation(predicate: NSPredicate(format: "value CONTAINS %@", "52"), object: caloriesTile)
        XCTAssertEqual(XCTWaiter().wait(for: [tileShowsCalories], timeout: 45), .completed, "Expected the nutrition panel to show 52 kcal")

        let status = app.staticTexts["mealSaveStatus"]
        let saved = XCTNSPredicateExpectation(predicate: NSPredicate(format: "label == %@", "All changes saved"), object: status)
        XCTAssertEqual(XCTWaiter().wait(for: [saved], timeout: 45), .completed, "Expected the status line to say all changes saved")

        // Leave no session in the Keychain for the next test.
        app.buttons["mealSheetDoneButton"].tap()
        signOutButton(in: app).tap()
        XCTAssertTrue(app.buttons["showRegisterButton"].waitForExistence(timeout: 45), "Expected the welcome screen after signing out")
    }
}
