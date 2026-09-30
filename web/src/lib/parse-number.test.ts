import { describe, expect, it } from "vitest";
import { parseDecimal } from "./parse-number";

describe("parseDecimal", () => {
  it.each([
    ["80", 80],
    ["1.5", 1.5],
    ["1,5", 1.5],
    [" 0.25 ", 0.25],
    [".5", 0.5],
    ["3.", 3],
    ["0", 0],
  ])("reads %j as %s", (text, value) => {
    expect(parseDecimal(text)).toEqual({ ok: true, value });
  });

  it("treats blank as no value, not as zero", () => {
    expect(parseDecimal("")).toEqual({ ok: true, value: null });
    expect(parseDecimal("   ")).toEqual({ ok: true, value: null });
  });

  it.each(["abc", "-2", "+2", "1e3", "0x10", "1.2.3", "1,2,3", "NaN", "Infinity", "12 g"])("rejects %j", (text) => {
    expect(parseDecimal(text)).toEqual({ ok: false });
  });
});
