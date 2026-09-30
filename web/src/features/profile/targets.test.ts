import { describe, expect, it } from "vitest";
import { TARGET_FIELDS, parseTargets } from "./targets";

function form(values: Partial<Record<keyof typeof TARGET_FIELDS, string>>) {
  const data = new FormData();
  for (const key of Object.keys(TARGET_FIELDS) as (keyof typeof TARGET_FIELDS)[]) data.set(TARGET_FIELDS[key], values[key] ?? "");
  return data;
}

describe("parseTargets", () => {
  it("reads all four, with a comma as a decimal point", () => {
    expect(parseTargets(form({ calories: "2000", protein: "120,5", carbs: "250", fat: "70" }))).toEqual({
      value: { target_kcal: 2000, target_protein_g: 120.5, target_carbs_g: 250, target_fat_g: 70 },
    });
  });

  it("turns a blank field into null, which clears that target", () => {
    expect(parseTargets(form({ calories: "2000" }))).toEqual({ value: { target_kcal: 2000, target_protein_g: null, target_carbs_g: null, target_fat_g: null } });
    expect(parseTargets(form({}))).toEqual({ value: { target_kcal: null, target_protein_g: null, target_carbs_g: null, target_fat_g: null } });
  });

  it("allows zero for a macro, not for calories", () => {
    expect(parseTargets(form({ protein: "0", carbs: "0", fat: "0" }))).toEqual({ value: { target_kcal: null, target_protein_g: 0, target_carbs_g: 0, target_fat_g: 0 } });
    expect(parseTargets(form({ calories: "0" }))).toEqual({ errors: { calories: "Calories must be more than 0 and at most 20000." } });
  });

  it("holds each target to the API's limits, inclusive at the top", () => {
    expect(parseTargets(form({ calories: "20000", protein: "2000", carbs: "5000", fat: "2000" }))).toHaveProperty("value");
    expect(parseTargets(form({ calories: "20001", protein: "2001", carbs: "5001", fat: "2001" }))).toEqual({
      errors: {
        calories: "Calories must be more than 0 and at most 20000.",
        protein: "Protein must be at most 2000 g.",
        carbs: "Carbohydrates must be at most 5000 g.",
        fat: "Fat must be at most 2000 g.",
      },
    });
  });

  it.each(["abc", "-5", "1e3", "12 g", "1.2.3"])("refuses %j as not a number and names the field", (text) => {
    expect(parseTargets(form({ fat: text }))).toEqual({ errors: { fat: "Enter a number, for example 70." } });
  });

  it("reports every bad field at once", () => {
    const result = parseTargets(form({ calories: "x", protein: "y" }));
    expect(result).toEqual({ errors: { calories: "Enter a number, for example 2000.", protein: "Enter a number, for example 70." } });
  });
});
