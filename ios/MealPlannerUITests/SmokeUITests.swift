import XCTest

final class SmokeUITests: XCTestCase {
    func testAppLaunches() throws {
        let app = XCUIApplication()
        app.launch()
        XCTAssertTrue(app.staticTexts["MealPlanner"].waitForExistence(timeout: 5))
    }
}
