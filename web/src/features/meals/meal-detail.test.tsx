// @vitest-environment jsdom
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { fakeApi, json, problem } from "@/test/fake-api";
import { makeMeal, nutrients } from "@/test/fixtures";
import { renderWithClient } from "@/test/render";
import { MealDetail } from "./meal-detail";

const push = vi.fn();
vi.mock("next/navigation", () => ({ useRouter: () => ({ push: (url: string) => push(url), replace: vi.fn() }) }));

afterEach(() => push.mockReset());

const line = { id: "l1", ingredient_id: "ing-oats", ingredient_name: "Rolled oats", ingredient_category: "grains_bread" as const, quantity: 80, unit: "g" as const, position: 0 };

describe("MealDetail", () => {
  it("opens my own meal in the editor", async () => {
    fakeApi({ "GET /meals/:id": () => json(makeMeal({ ingredients: [line] })), "GET /partner": () => problem(404, "partner_not_linked") });
    renderWithClient(<MealDetail id="m1" />);
    expect(await screen.findByLabelText("Name", { exact: true })).toHaveValue("Oat bowl");
    expect(screen.getByRole("heading", { name: "Edit meal" })).toBeInTheDocument();
  });

  it("shows the partner's meal read-only, with nutrition and a copy button, and no editor", async () => {
    const shared = makeMeal({
      id: "p1",
      name: "Lentil soup",
      is_owner: false,
      notes: "Freezes well",
      ingredients: [line],
      nutrition_per_serving: nutrients({ calories: 210 }),
    });
    const fake = fakeApi({
      "GET /meals/:id": () => json(shared),
      "POST /meals/:id/copy": () => json(makeMeal({ id: "copy1", name: "Lentil soup" }), 201),
    });
    renderWithClient(<MealDetail id="p1" />);

    expect(await screen.findByRole("heading", { name: "Lentil soup" })).toBeInTheDocument();
    expect(screen.getByText("Freezes well")).toBeInTheDocument();
    expect(screen.getByText("Rolled oats")).toBeInTheDocument();
    expect(screen.getByText("80 g")).toBeInTheDocument();
    expect(within(screen.getByRole("region", { name: "Nutrition" })).getByText("210 kcal")).toBeInTheDocument();
    expect(screen.queryByLabelText("Name", { exact: true })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Delete meal" })).not.toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: /Copy to my library/ }));
    await waitFor(() => expect(push).toHaveBeenCalledWith("/meals/copy1"));
    expect(fake.callsTo("POST", "/meals/p1/copy")).toHaveLength(1);
  });

  it("says a meal is not available, with a way back, when it is gone or no longer shared", async () => {
    fakeApi({ "GET /meals/:id": () => problem(404, "not_found") });
    renderWithClient(<MealDetail id="gone" />);
    expect(await screen.findByText("That isn't available. It may have been removed, or it isn't shared with you.")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /Meals/ })).toHaveAttribute("href", "/meals");
    expect(screen.queryByRole("button", { name: "Try again" })).not.toBeInTheDocument();
  });

  it("offers a retry when the meal cannot be loaded for another reason", async () => {
    let fail = true;
    fakeApi({
      "GET /meals/:id": () => (fail ? problem(500, "internal_error") : json(makeMeal())),
      "GET /partner": () => problem(404, "partner_not_linked"),
    });
    renderWithClient(<MealDetail id="m1" />);
    expect(await screen.findByText("Something went wrong. Try again.")).toBeInTheDocument();
    fail = false;
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByLabelText("Name", { exact: true })).toBeInTheDocument();
  });
});
