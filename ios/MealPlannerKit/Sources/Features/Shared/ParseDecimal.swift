import Foundation

/// A number as a person types it. `.blank` means nothing was entered (an optional field left empty),
/// `.invalid` means text that is not a plain decimal.
public enum ParsedDecimal: Equatable, Sendable {
    case blank
    case value(Double)
    case invalid
}

/// Reads a number the way web's `parseDecimal` does: a comma is a decimal point, blank is "nothing
/// entered", and only ASCII digits with at most one point are valid. Signs, exponents, hex and stray
/// text are invalid, so nothing surprising reaches the API. Deliberately does not use `NumberFormatter`:
/// behaviour is identical on every device locale and deterministic in tests.
public func parseDecimal(_ text: String) -> ParsedDecimal {
    var trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
    if trimmed.isEmpty { return .blank }
    if let comma = trimmed.firstIndex(of: ",") {
        trimmed.replaceSubrange(comma...comma, with: ".")
    }
    var digits = 0
    var points = 0
    for character in trimmed {
        if character.isASCII, character.isNumber {
            digits += 1
        } else if character == "." {
            points += 1
        } else {
            return .invalid
        }
    }
    guard digits > 0, points <= 1 else { return .invalid }
    let normalized = (trimmed.hasPrefix(".") ? "0" : "") + trimmed + (trimmed.hasSuffix(".") ? "0" : "")
    guard let value = Double(normalized), value.isFinite else { return .invalid }
    return .value(value)
}

/// A number as editable text: "2" rather than "2.0", "0.5" as is.
public func plainNumber(_ value: Double) -> String {
    if value == value.rounded(), abs(value) < 1e15 { return String(Int(value)) }
    return String(value)
}
