// @vitest-environment jsdom
import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { nutrients, unknownNutrients } from "@/test/fixtures";
import { TARGETS } from "@/test/plan-fixtures";
import { MacroRings } from "./macro-rings";

const ring = (name: string) => screen.getByRole("group", { name });
const fraction = (name: string) => ring(name).querySelector("circle[data-fraction]")?.getAttribute("data-fraction");

describe("MacroRings", () => {
  it("fills each ring by its share of the target and says the numbers in words", () => {
    render(<MacroRings nutrition={nutrients({ calories: 1200, protein: 90, carbohydrates: 150, fat: 40 })} targets={TARGETS} />);
    expect(within(ring("Calories")).getByText("1,200 kcal")).toBeInTheDocument();
    expect(within(ring("Calories")).getByText("60% of 2,000 kcal")).toBeInTheDocument();
    expect(fraction("Calories")).toBe("0.600");
    expect(within(ring("Protein")).getByText("75% of 120 g")).toBeInTheDocument();
    expect(fraction("Protein")).toBe("0.750");
    expect(within(ring("Carbohydrates")).getByText("60% of 250 g")).toBeInTheDocument();
    expect(within(ring("Fat")).getByText("57% of 70 g")).toBeInTheDocument();
  });

  it("fills the ring and says so when the day is over target, keeping the real percent", () => {
    render(<MacroRings nutrition={nutrients({ calories: 2500 })} targets={TARGETS} />);
    expect(fraction("Calories")).toBe("1.000");
    expect(within(ring("Calories")).getByText("125% of 2,000 kcal")).toBeInTheDocument();
    expect(within(ring("Calories")).getByText("Over target")).toBeInTheDocument();
    expect(within(ring("Protein")).queryByText("Over target")).not.toBeInTheDocument();
  });

  it.each([
    ["missing", null],
    ["zero", 0],
    ["negative", -100],
  ] as const)("shows a %s target as no target, with the amount and an empty ring, never NaN or Infinity", (_name, target) => {
    render(<MacroRings nutrition={nutrients({ calories: 1200 })} targets={{ ...TARGETS, target_kcal: target }} />);
    expect(within(ring("Calories")).getByText("1,200 kcal")).toBeInTheDocument();
    expect(within(ring("Calories")).getByText("No target set")).toBeInTheDocument();
    expect(fraction("Calories")).toBe("0.000");
    expect(document.body.textContent).not.toMatch(/NaN|Infinity/);
  });

  it("shows an unknown amount as a dash with the target, and an empty ring, never as zero", () => {
    render(<MacroRings nutrition={unknownNutrients()} targets={TARGETS} />);
    const kcal = within(ring("Calories"));
    expect(kcal.getByText("—")).toBeInTheDocument();
    expect(kcal.getByText("Target 2,000 kcal")).toBeInTheDocument();
    expect(fraction("Calories")).toBe("0.000");
    expect(kcal.queryByText(/^0 /)).not.toBeInTheDocument();
  });

  it("shows a genuine zero as zero with an empty ring", () => {
    render(<MacroRings nutrition={nutrients()} targets={TARGETS} />);
    expect(within(ring("Calories")).getByText("0 kcal")).toBeInTheDocument();
    expect(within(ring("Calories")).getByText("0% of 2,000 kcal")).toBeInTheDocument();
    expect(fraction("Calories")).toBe("0.000");
  });

  it("is labelled, and marks itself busy while its numbers are out of date", () => {
    render(<MacroRings nutrition={nutrients()} targets={TARGETS} stale label="Today's totals" />);
    expect(screen.getByRole("region", { name: "Today's totals" })).toHaveAttribute("aria-busy", "true");
  });
});
