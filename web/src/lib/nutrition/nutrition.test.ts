import { describe, expect, it } from "vitest";
import {
  DAILY_VALUES,
  MACRO_KEYS,
  NO_DATA,
  NUTRIENTS,
  NUTRIENT_GROUPS,
  dailyValuePercent,
  formatAmount,
  formatDailyValue,
  hasDailyValue,
  nutrientInfo,
} from ".";

describe("nutrient catalog", () => {
  it("lists the 18 tracked nutrients once each, in the API's order", () => {
    expect(NUTRIENTS.map((n) => n.key)).toEqual([
      "calories",
      "protein",
      "carbohydrates",
      "sugar",
      "fibre",
      "fat",
      "saturated_fat",
      "sodium",
      "potassium",
      "calcium",
      "iron",
      "magnesium",
      "zinc",
      "vitamin_a",
      "vitamin_c",
      "vitamin_d",
      "vitamin_b12",
      "folate",
    ]);
  });

  it("puts every nutrient in a known group and keeps the macro keys in the catalog", () => {
    for (const n of NUTRIENTS) expect(NUTRIENT_GROUPS).toContain(n.group);
    for (const key of MACRO_KEYS) expect(nutrientInfo(key).group).toBe("Macronutrients");
  });

  it("uses the units the API documents", () => {
    expect(nutrientInfo("calories").unit).toBe("kcal");
    expect(nutrientInfo("protein").unit).toBe("g");
    expect(nutrientInfo("sodium").unit).toBe("mg");
    expect(nutrientInfo("vitamin_c").unit).toBe("mg");
    expect(nutrientInfo("vitamin_d").unit).toBe("µg");
    expect(nutrientInfo("folate").unit).toBe("µg");
  });
});

describe("daily values", () => {
  it("has a reference amount only for the nutrients a food label gives a percentage for", () => {
    expect(Object.keys(DAILY_VALUES).sort()).toEqual(
      ["calcium", "fibre", "folate", "iron", "magnesium", "potassium", "saturated_fat", "sodium", "vitamin_a", "vitamin_b12", "vitamin_c", "vitamin_d", "zinc"].sort(),
    );
    expect(hasDailyValue("sodium")).toBe(true);
    expect(hasDailyValue("protein")).toBe(false);
  });

  it("is 100% at the reference amount", () => {
    for (const [key, dv] of Object.entries(DAILY_VALUES)) {
      expect(dailyValuePercent(key as keyof typeof DAILY_VALUES, dv)).toBeCloseTo(100, 10);
    }
  });

  it("scales linearly and treats zero as zero percent", () => {
    expect(dailyValuePercent("sodium", 1150)).toBeCloseTo(50, 10);
    expect(dailyValuePercent("vitamin_b12", 0)).toBe(0);
  });

  it("has no percentage for an unknown amount or a nutrient without a reference", () => {
    expect(dailyValuePercent("sodium", null)).toBeNull();
    expect(dailyValuePercent("protein", 20)).toBeNull();
  });
});

describe("formatAmount", () => {
  it.each([
    [152.04, "kcal", "152 kcal"],
    [5.2, "g", "5.2 g"],
    [10.4, "g", "10 g"],
    [2300, "mg", "2,300 mg"],
    [2.4, "µg", "2.4 µg"],
    [0, "g", "0 g"],
    [0.049, "g", "0 g"],
  ] as const)("formats %s %s as %s", (amount, unit, text) => {
    expect(formatAmount(amount, unit)).toBe(text);
  });

  it("never shows an unknown amount as zero", () => {
    expect(formatAmount(null, "kcal")).toBe(NO_DATA);
    expect(formatAmount(null, "g")).not.toContain("0");
  });
});

describe("formatDailyValue", () => {
  it("rounds to a whole percent", () => {
    expect(formatDailyValue("sodium", 1150)).toBe("50%");
    expect(formatDailyValue("vitamin_c", 45.4)).toBe("50%");
    expect(formatDailyValue("calcium", 0)).toBe("0%");
  });

  it("shows the no-data dash for an unknown amount", () => {
    expect(formatDailyValue("iron", null)).toBe(NO_DATA);
  });
});
