import { describe, expect, it } from "vitest";
import { safeNext } from "./safe-next";

describe("safeNext", () => {
  it("keeps an in-app path with its query", () => {
    expect(safeNext("/meals?tab=partner")).toBe("/meals?tab=partner");
    expect(safeNext("/shopping/abc")).toBe("/shopping/abc");
  });

  it("falls back to /today when missing", () => {
    expect(safeNext(null)).toBe("/today");
    expect(safeNext(undefined)).toBe("/today");
    expect(safeNext("")).toBe("/today");
  });

  it.each(["//evil.test", "/\\evil.test", "https://evil.test/x", "http://evil.test", "javascript:alert(1)", "evil.test", "\\\\evil.test", "/\t/evil.test"])(
    "refuses %j",
    (raw) => {
      expect(safeNext(raw)).toBe("/today");
    },
  );
});
