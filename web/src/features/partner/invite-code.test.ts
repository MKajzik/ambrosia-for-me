import { describe, expect, it } from "vitest";
import { formatDate, formatInviteCode, formatWhen } from "./invite-code";

describe("formatInviteCode", () => {
  it("groups an 8-character code in fours, in capitals", () => {
    expect(formatInviteCode("abcdefgh")).toBe("ABCD-EFGH");
    expect(formatInviteCode("K7M2QX9R")).toBe("K7M2-QX9R");
  });

  it("copes with a code of another length, and with what is not a code", () => {
    expect(formatInviteCode("ABC")).toBe("ABC");
    expect(formatInviteCode("ABCDEFGHJ")).toBe("ABCD-EFGH-J");
    expect(formatInviteCode("")).toBe("");
  });
});

describe("formatDate and formatWhen", () => {
  it("write a date the way a person would read it", () => {
    expect(formatDate("2026-01-15T12:00:00Z")).toBe("Jan 15, 2026");
    expect(formatWhen("2026-01-15T12:00:00Z")).toMatch(/^Jan 15, 2026, \d{1,2}:\d{2}\s?(AM|PM)$/);
  });
});
