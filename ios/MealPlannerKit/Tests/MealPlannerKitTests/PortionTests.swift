import Testing
@testable import Features

@Suite
struct PortionTests {
    @Test(arguments: [("1", 1.0), ("0.5", 0.5), ("1,5", 1.5), ("100", 100.0), ("0.01", 0.01), (" 2 ", 2.0), ("2.", 2.0)])
    func valid(text: String, expected: Double) {
        #expect(Portion.value(text) == expected)
    }

    @Test(arguments: ["0", "100.01", "101", "-1", "", "abc", "1e2", "0,0"])
    func invalid(text: String) {
        #expect(Portion.value(text) == nil)
    }

    @Test("The message names both limits")
    func message() {
        #expect(Portion.message == "Portion must be more than 0 and at most 100.")
    }
}
