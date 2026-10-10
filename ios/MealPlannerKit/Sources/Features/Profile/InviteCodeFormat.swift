/// An invite code as people read it aloud: capitals, in groups of four. Spaces and dashes in what was given are ignored.
public enum InviteCodeFormat {
    private static func characters(_ code: String) -> [Character] {
        code.uppercased().filter { !$0.isWhitespace && $0 != "-" }.map { $0 }
    }

    public static func display(_ code: String) -> String {
        let letters = characters(code)
        return stride(from: 0, to: letters.count, by: 4)
            .map { String(letters[$0 ..< min($0 + 4, letters.count)]) }
            .joined(separator: "-")
    }

    /// For VoiceOver: "A B C D 2 3 4 5", so the code is not read as a word.
    public static func spoken(_ code: String) -> String {
        characters(code).map(String.init).joined(separator: " ")
    }
}
