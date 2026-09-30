import { describe, expect, it } from "vitest";
import { ApiError, fieldMessage, problemMessage } from "./problem";

const err = (code: string, status = 409) => new ApiError({ status, code });

describe("problemMessage for the meal screens", () => {
  it.each([
    ["meal_in_use", /plan or in a diet template/],
    ["unit_not_convertible", /unit/],
    ["partner_not_linked", /partner/],
    ["not_found", /isn't available/],
    ["ingredient_in_use", /still uses/],
    ["invalid_ingredient", /ingredients/],
  ])("explains %s in words", (code, pattern) => {
    expect(problemMessage(err(code))).toMatch(pattern);
  });

  it("still gives an unknown code the generic line", () => {
    expect(problemMessage(err("something_new", 500))).toBe("Something went wrong. Try again.");
  });
});

describe("fieldMessage for numbers", () => {
  it("explains out-of-range and wrong-type values", () => {
    expect(fieldMessage("servings", "out_of_range")).toBe("That number is out of range.");
    expect(fieldMessage("servings", "invalid_type")).toBe("Enter a number.");
  });
});
