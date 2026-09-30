import { describe, expect, it } from "vitest";
import { validateRange } from "./range";

describe("validateRange", () => {
  it("accepts a week, a single day and exactly 92 days", () => {
    expect(validateRange("2026-09-28", "2026-10-04")).toEqual({ ok: true });
    expect(validateRange("2026-09-28", "2026-09-28")).toEqual({ ok: true });
    expect(validateRange("2026-01-01", "2026-04-02")).toEqual({ ok: true });
  });

  it("refuses 93 days", () => {
    expect(validateRange("2026-01-01", "2026-04-03")).toEqual({ ok: false, error: "Pick at most 92 days." });
  });

  it("refuses an end before the start", () => {
    expect(validateRange("2026-10-04", "2026-09-28")).toEqual({ ok: false, error: "The end date must be on or after the start date." });
  });

  it.each([
    ["", "2026-10-04"],
    ["2026-09-28", ""],
    ["", ""],
    ["2026-02-30", "2026-03-05"],
    ["tomorrow", "2026-03-05"],
  ])("refuses %j to %j as not a range", (from, to) => {
    expect(validateRange(from, to)).toEqual({ ok: false, error: "Choose a start and end date." });
  });

  it("counts days across a daylight-saving change and a year end", () => {
    expect(validateRange("2026-03-01", "2026-05-31")).toEqual({ ok: true });
    expect(validateRange("2026-12-01", "2027-02-28")).toEqual({ ok: true });
    expect(validateRange("2026-12-01", "2027-03-03")).toEqual({ ok: false, error: "Pick at most 92 days." });
  });
});
