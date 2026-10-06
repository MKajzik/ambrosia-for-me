/// A portion typed by a person: more than 0 and at most 100 (the API's limits for a plan entry and a template slot).
public enum Portion {
    public static let message = "Portion must be more than 0 and at most 100."

    public static func value(_ text: String) -> Double? {
        guard case .value(let portion) = parseDecimal(text), portion > 0, portion <= 100 else { return nil }
        return portion
    }
}
