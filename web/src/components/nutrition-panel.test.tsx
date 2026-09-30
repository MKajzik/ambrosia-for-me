// @vitest-environment jsdom
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { nutrients, unknownNutrients } from "@/test/fixtures";
import { NutritionPanel } from "./nutrition-panel";

describe("NutritionPanel", () => {
  it("shows the four macros per serving", () => {
    render(<NutritionPanel nutrition={nutrients({ calories: 152.04, protein: 5.2, carbohydrates: 24, fat: 2.8 })} />);
    const panel = screen.getByRole("region", { name: "Nutrition" });
    expect(within(panel).getByText("Per serving")).toBeInTheDocument();
    expect(within(panel).getByText("152 kcal")).toBeInTheDocument();
    expect(within(panel).getByText("5.2 g")).toBeInTheDocument();
    expect(within(panel).getByText("24 g")).toBeInTheDocument();
    expect(within(panel).getByText("2.8 g")).toBeInTheDocument();
  });

  it("shows an unknown amount as a dash with a hint, never as zero", () => {
    render(<NutritionPanel nutrition={unknownNutrients({ calories: 120 })} />);
    expect(screen.getByText("120 kcal")).toBeInTheDocument();
    expect(screen.getAllByText("—")).toHaveLength(3);
    expect(screen.getByText(/some ingredients lack data/i)).toBeInTheDocument();
    expect(screen.queryByText(/^0 (g|kcal)$/)).not.toBeInTheDocument();
  });

  it("shows a genuine zero as zero, with no dash and no hint (a meal with no ingredients)", () => {
    render(<NutritionPanel nutrition={nutrients()} />);
    expect(screen.getByText("0 kcal")).toBeInTheDocument();
    expect(screen.queryByText("—")).not.toBeInTheDocument();
    expect(screen.queryByText(/some ingredients lack data/i)).not.toBeInTheDocument();
  });

  it("expands to every nutrient, with a percent of the daily value only where a reference exists", async () => {
    render(<NutritionPanel nutrition={nutrients({ protein: 20, vitamin_c: 45, sodium: 1150 })} />);
    expect(screen.queryByText("Vitamin C")).not.toBeInTheDocument();

    const toggle = screen.getByRole("button", { name: "All nutrients" });
    expect(toggle).toHaveAttribute("aria-expanded", "false");
    await userEvent.click(toggle);
    expect(toggle).toHaveAttribute("aria-expanded", "true");

    const vitamins = within(screen.getByRole("group", { name: "Vitamins" }));
    const vitaminC = vitamins.getByText("Vitamin C").closest("div")!;
    expect(within(vitaminC).getByText("45 mg")).toBeInTheDocument();
    expect(within(vitaminC).getByText("50%")).toBeInTheDocument();

    const minerals = within(screen.getByRole("group", { name: "Minerals" }));
    expect(within(minerals.getByText("Sodium").closest("div")!).getByText("50%")).toBeInTheDocument();

    const macros = within(screen.getByRole("group", { name: "Macronutrients" }));
    const protein = macros.getByText("Protein").closest("div")!;
    expect(within(protein).getByText("20 g")).toBeInTheDocument();
    expect(protein).not.toHaveTextContent("%");
    expect(screen.getAllByRole("group")).toHaveLength(3);
  });

  it("dashes both the amount and the percent of an unknown micronutrient", async () => {
    render(<NutritionPanel nutrition={unknownNutrients({ calories: 10, protein: 1, carbohydrates: 1, fat: 1 })} />);
    await userEvent.click(screen.getByRole("button", { name: "All nutrients" }));
    const iron = within(screen.getByRole("group", { name: "Minerals" })).getByText("Iron").closest("div")!;
    expect(within(iron).getAllByText("—")).toHaveLength(2);
  });

  it("marks itself busy while its numbers are out of date", () => {
    render(<NutritionPanel nutrition={nutrients()} stale />);
    expect(screen.getByRole("region", { name: "Nutrition" })).toHaveAttribute("aria-busy", "true");
  });
});
