import { describe, expect, it } from "vitest";
import { ApiError, problemMessage } from "./problem";

const err = (code: string, status = 409) => new ApiError({ status, code });

describe("problemMessage for the plan and template screens", () => {
  it.each([
    ["plan_conflict", /already have meals/],
    ["plan_range_too_long", /date range/],
    ["plan_range_invalid", /date range/],
    ["day_index_out_of_range", /days/],
    ["duplicate_slot", /already has/],
    ["invalid_meal", /isn't available/],
  ])("explains %s in words", (code, pattern) => {
    expect(problemMessage(err(code))).toMatch(pattern);
  });
});
