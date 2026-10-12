import Testing
@testable import Features

@Suite
struct InviteCodeFormatTests {
    @Test("An 8-character code is shown in capitals in groups of four")
    func display() {
        #expect(InviteCodeFormat.display("abcd2345") == "ABCD-2345")
        #expect(InviteCodeFormat.display("ABCD2345") == "ABCD-2345")
    }

    @Test("Short, long, spaced and already-dashed input is handled")
    func edges() {
        #expect(InviteCodeFormat.display("") == "")
        #expect(InviteCodeFormat.display("ABC") == "ABC")
        #expect(InviteCodeFormat.display("ABCDEFGHJ") == "ABCD-EFGH-J")
        #expect(InviteCodeFormat.display(" abcd-2345 ") == "ABCD-2345")
    }

    @Test("Spoken, a code is read character by character")
    func spoken() {
        #expect(InviteCodeFormat.spoken("abcd-2345") == "A B C D 2 3 4 5")
    }
}
