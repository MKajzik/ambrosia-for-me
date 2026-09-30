import { describe, expect, it } from "vitest";
import { nutrients, unknownNutrients } from "@/test/fixtures";
import { scaleNutrition, sumNutrition, targetProgress, zeroNutrition } from ".";

describe("zeroNutrition and sumNutrition", () => {
  it("is all zeros, and the sum of nothing is all zeros", () => {
    expect(zeroNutrition()).toEqual(nutrients());
    expect(sumNutrition([])).toEqual(nutrients());
  });

  it("adds each nutrient across the list", () => {
    const total = sumNutrition([nutrients({ calories: 300, protein: 10 }), nutrients({ calories: 450.5, iron: 2 }), nutrients({ calories: 0 })]);
    expect(total.calories).toBeCloseTo(750.5, 10);
    expect(total.protein).toBe(10);
    expect(total.iron).toBe(2);
    expect(total.sodium).toBe(0);
  });

  it("is unknown for a nutrient that any item does not know, and known for the rest", () => {
    const total = sumNutrition([nutrients({ calories: 300, iron: 2 }), nutrients({ calories: 100, iron: null })]);
    expect(total.iron).toBeNull();
    expect(total.calories).toBe(400);
  });

  it("stays unknown once unknown, whatever comes after", () => {
    const total = sumNutrition([unknownNutrients(), nutrients({ calories: 100 })]);
    expect(total.calories).toBeNull();
  });
});

describe("scaleNutrition", () => {
  it("multiplies known amounts and leaves unknown ones unknown", () => {
    const scaled = scaleNutrition(nutrients({ calories: 700, protein: null }), 1 / 7);
    expect(scaled.calories).toBeCloseTo(100, 10);
    expect(scaled.protein).toBeNull();
  });
});

describe("targetProgress", () => {
  it("is the share of the target reached", () => {
    expect(targetProgress(1200, 2000)).toEqual({ fraction: 0.6, percent: 60, over: false });
    expect(targetProgress(0, 2000)).toEqual({ fraction: 0, percent: 0, over: false });
    expect(targetProgress(2000, 2000)).toEqual({ fraction: 1, percent: 100, over: false });
  });

  it("fills the ring but keeps the real percent when the target is exceeded", () => {
    expect(targetProgress(2500, 2000)).toEqual({ fraction: 1, percent: 125, over: true });
  });

  it.each([
    [1200, null],
    [1200, 0],
    [1200, -5],
    [null, 2000],
    [null, null],
    [Number.NaN, 2000],
    [1200, Number.NaN],
  ])("has no progress for value %s and target %s (never NaN or Infinity)", (value, target) => {
    expect(targetProgress(value, target)).toBeNull();
  });
});
