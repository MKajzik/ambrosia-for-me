import Testing
@testable import Features

@Suite
struct ParseDecimalTests {
    @Test(arguments: [
        ("12", 12.0), ("12.5", 12.5), ("12,5", 12.5), (" 7 ", 7.0), ("5.", 5.0),
        (".5", 0.5), ("0", 0.0), ("007", 7.0), ("1,", 1.0),
    ])
    func parsesNumbers(text: String, expected: Double) {
        #expect(parseDecimal(text) == .value(expected))
    }

    @Test(arguments: ["", "   ", "\n"])
    func blankMeansNothingEntered(text: String) {
        #expect(parseDecimal(text) == .blank)
    }

    @Test(arguments: ["-1", "+1", "1e5", "0x10", "1.2.3", "1,2,3", "abc", "1 000", ".", ",", "٣", "1_0", "NaN", "inf", "12 g"])
    func rejectsEverythingElse(text: String) {
        #expect(parseDecimal(text) == .invalid)
    }

    @Test("plainNumber drops a pointless .0 and keeps real fractions")
    func plain() {
        #expect(plainNumber(2) == "2")
        #expect(plainNumber(150) == "150")
        #expect(plainNumber(0.5) == "0.5")
        #expect(plainNumber(2.25) == "2.25")
    }
}
