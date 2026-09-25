import { describe, expect, it } from "vitest";
import { NAV_ITEMS, isActive } from "./nav";

describe("nav", () => {
  it("lists the five areas from the spec", () => {
    expect(NAV_ITEMS.map((i) => i.label)).toEqual(["Today", "Plan", "Meals", "Shopping", "Profile"]);
  });

  it("highlights an area for its own pages, not for lookalike prefixes", () => {
    expect(isActive("/meals", "/meals")).toBe(true);
    expect(isActive("/meals/abc", "/meals")).toBe(true);
    expect(isActive("/mealsx", "/meals")).toBe(false);
    expect(isActive("/plan", "/meals")).toBe(false);
  });
});
