// @vitest-environment jsdom
import { useQuery } from "@tanstack/react-query";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { fakeApi, json, noContent, problem } from "@/test/fake-api";
import { makeIngredient, makeMeal, nutrients, partnership } from "@/test/fixtures";
import { renderWithClient } from "@/test/render";
import { MealEditor } from "./meal-editor";
import { mealKeys, type Meal } from "./queries";

const replace = vi.fn();
vi.mock("next/navigation", () => ({ useRouter: () => ({ replace: (url: string) => replace(url), push: vi.fn() }) }));

afterEach(() => replace.mockReset());

const oats = makeIngredient();
const line = { id: "l1", ingredient_id: oats.id, ingredient_name: oats.name, ingredient_category: oats.category, quantity: 80, unit: "g" as const, position: 0 };
const startMeal = () => makeMeal({ ingredients: [line], nutrition_per_serving: nutrients({ calories: 152 }) });

/** Feeds the editor the way the page does: the meal comes from the query cache, which each save updates. */
function Harness({ initial, delay }: { initial: Meal; delay: number }) {
  const { data } = useQuery({ queryKey: mealKeys.detail(initial.id), queryFn: () => initial, initialData: initial, staleTime: Infinity });
  return <MealEditor meal={data} autosaveDelayMs={delay} />;
}

function setup(routes: Parameters<typeof fakeApi>[0] = {}, { delay = 10, meal = startMeal() } = {}) {
  const fake = fakeApi({
    "GET /partner": () => problem(404, "partner_not_linked"),
    "GET /ingredients": () => json({ items: [oats], next_cursor: null }),
    ...routes,
  });
  const view = renderWithClient(<Harness initial={meal} delay={delay} />);
  return { fake, meal, ...view };
}

const put = (fake: ReturnType<typeof fakeApi>) => fake.callsTo("PUT", "/meals/m1/ingredients");
const patch = (fake: ReturnType<typeof fakeApi>) => fake.callsTo("PATCH", "/meals/m1");
const nothingElseHappens = () => new Promise((resolve) => setTimeout(resolve, 120));

describe("MealEditor", () => {
  it("shows the meal as saved, with the nutrition the server computed", () => {
    setup();
    expect(screen.getByLabelText("Name", { exact: true })).toHaveValue("Oat bowl");
    expect(screen.getByLabelText("Servings")).toHaveValue("2");
    expect(screen.getByLabelText("Quantity of Rolled oats")).toHaveValue("80");
    expect(screen.getByLabelText("Unit for Rolled oats")).toHaveValue("g");
    expect(within(screen.getByRole("region", { name: "Nutrition" })).getByText("152 kcal")).toBeInTheDocument();
    expect(screen.getByText("All changes saved")).toBeInTheDocument();
  });

  it("saves a rename after a pause, sending only what changed", async () => {
    const { fake } = setup({ "PATCH /meals/:id": () => json({ ...startMeal(), name: "Porridge" }) });
    fireEvent.change(screen.getByLabelText("Name", { exact: true }), { target: { value: "Porridge" } });

    await waitFor(() => expect(patch(fake)).toHaveLength(1));
    expect(patch(fake)[0]?.body).toEqual({ name: "Porridge" });
    expect(put(fake)).toHaveLength(0);
    expect(await screen.findByText("All changes saved")).toBeInTheDocument();
  });

  it("adds an ingredient from the search and shows the nutrition the server sends back", async () => {
    const updated = makeMeal({
      ingredients: [line, { ...line, id: "l2", position: 1 }],
      nutrition_per_serving: nutrients({ calories: 380 }),
    });
    const { fake } = setup({ "PUT /meals/:id/ingredients": () => json(updated) });

    await userEvent.click(screen.getByRole("combobox", { name: "Add an ingredient" }));
    await userEvent.click(await screen.findByRole("option", { name: /Rolled oats/ }));

    await waitFor(() => expect(put(fake)).toHaveLength(1));
    expect(put(fake)[0]?.body).toEqual({
      items: [
        { ingredient_id: "ing-oats", quantity: 80, unit: "g" },
        { ingredient_id: "ing-oats", quantity: 100, unit: "g" },
      ],
    });
    expect(await screen.findByText("380 kcal")).toBeInTheDocument();
    expect(screen.getAllByLabelText(/Quantity of Rolled oats/)).toHaveLength(2);
  });

  it("removes an ingredient and saves the shorter list", async () => {
    const { fake } = setup({ "PUT /meals/:id/ingredients": () => json(makeMeal({ nutrition_per_serving: nutrients() })) });
    await userEvent.click(screen.getByRole("button", { name: "Remove Rolled oats" }));
    await waitFor(() => expect(put(fake)).toHaveLength(1));
    expect(put(fake)[0]?.body).toEqual({ items: [] });
    expect(screen.getByText(/No ingredients yet/)).toBeInTheDocument();
  });

  it("takes an amount typed with a comma", async () => {
    const { fake } = setup({ "PUT /meals/:id/ingredients": () => json(startMeal()) });
    fireEvent.change(screen.getByLabelText("Quantity of Rolled oats"), { target: { value: "1,5" } });
    await waitFor(() => expect(put(fake)).toHaveLength(1));
    expect(put(fake)[0]?.body).toEqual({ items: [{ ingredient_id: "ing-oats", quantity: 1.5, unit: "g" }] });
  });

  it("flags an amount or servings it cannot use, sends nothing, and keeps what was typed", async () => {
    const { fake } = setup();
    fireEvent.change(screen.getByLabelText("Quantity of Rolled oats"), { target: { value: "abc" } });
    fireEvent.change(screen.getByLabelText("Servings"), { target: { value: "0" } });

    expect(await screen.findByText("Enter an amount.")).toBeInTheDocument();
    expect(screen.getByText("Servings must be more than 0 and at most 1000.")).toBeInTheDocument();
    expect(screen.getByText("Fix the highlighted fields to save.")).toBeInTheDocument();
    await nothingElseHappens();
    expect(put(fake)).toHaveLength(0);
    expect(patch(fake)).toHaveLength(0);
    expect(screen.getByLabelText("Quantity of Rolled oats")).toHaveValue("abc");
  });

  it("explains a unit the ingredient cannot convert, keeps the draft, and does not retry by itself", async () => {
    const { fake } = setup({ "PUT /meals/:id/ingredients": () => problem(409, "unit_not_convertible") });
    await userEvent.selectOptions(screen.getByLabelText("Unit for Rolled oats"), "ml");

    expect(await screen.findByText(/can't be measured in that unit/)).toBeInTheDocument();
    expect(screen.getByLabelText("Unit for Rolled oats")).toHaveValue("ml");
    await nothingElseHappens();
    expect(put(fake)).toHaveLength(1);

    await userEvent.selectOptions(screen.getByLabelText("Unit for Rolled oats"), "g");
    await waitFor(() => expect(put(fake)).toHaveLength(1));
    expect(screen.queryByText(/can't be measured in that unit/)).not.toBeInTheDocument();
  });

  it("offers a retry after a failed save and saves the same draft again", async () => {
    let fail = true;
    const { fake } = setup({ "PATCH /meals/:id": () => (fail ? problem(500, "internal_error") : json({ ...startMeal(), name: "Porridge" })) });
    fireEvent.change(screen.getByLabelText("Name", { exact: true }), { target: { value: "Porridge" } });
    expect(await screen.findByText("Something went wrong. Try again.")).toBeInTheDocument();

    fail = false;
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByText("All changes saved")).toBeInTheDocument();
    expect(patch(fake)).toHaveLength(2);
  });

  it("saves a pending edit when the editor is closed before the pause is over", async () => {
    const { fake, unmount } = setup({ "PATCH /meals/:id": () => json({ ...startMeal(), name: "Porridge" }) }, { delay: 60_000 });
    fireEvent.change(screen.getByLabelText("Name", { exact: true }), { target: { value: "Porridge" } });
    expect(patch(fake)).toHaveLength(0);
    unmount();
    await waitFor(() => expect(patch(fake)).toHaveLength(1));
  });

  it("offers sharing only while linked with a partner, and saves the choice", async () => {
    const { fake } = setup({ "GET /partner": () => json(partnership()), "PATCH /meals/:id": () => json({ ...startMeal(), shared_with_partner: true }) });
    await userEvent.click(await screen.findByLabelText("Share with my partner"));
    await waitFor(() => expect(patch(fake)).toHaveLength(1));
    expect(patch(fake)[0]?.body).toEqual({ shared_with_partner: true });
  });

  it("hides the sharing choice when there is no partner", async () => {
    const { fake, queryClient } = setup();
    await waitFor(() => expect(fake.callsTo("GET", "/partner")).toHaveLength(1));
    await waitFor(() => expect(queryClient.getQueryState(["partner"])?.status).toBe("success"));
    expect(screen.queryByLabelText("Share with my partner")).not.toBeInTheDocument();
  });

  it("deletes the meal after a confirmation and returns to the library", async () => {
    const { fake } = setup({ "DELETE /meals/:id": () => noContent() });
    await userEvent.click(screen.getByRole("button", { name: "Delete meal" }));
    const dialog = await screen.findByRole("dialog", { name: "Delete this meal?" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Delete meal" }));

    await waitFor(() => expect(replace).toHaveBeenCalledWith("/meals"));
    expect(fake.callsTo("DELETE", "/meals/m1")).toHaveLength(1);
  });

  it("says where to remove a meal that a plan or template still uses, and stays put", async () => {
    setup({ "DELETE /meals/:id": () => problem(409, "meal_in_use") });
    await userEvent.click(screen.getByRole("button", { name: "Delete meal" }));
    const dialog = await screen.findByRole("dialog", { name: "Delete this meal?" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Delete meal" }));

    expect(await within(dialog).findByText(/scheduled in your plan or in a diet template/)).toBeInTheDocument();
    expect(replace).not.toHaveBeenCalled();
    expect(screen.getByRole("dialog", { name: "Delete this meal?" })).toBeInTheDocument();
  });
});
