// @vitest-environment jsdom
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { fakeApi, json, noContent, problem } from "@/test/fake-api";
import { makeIngredient, nutrients } from "@/test/fixtures";
import { renderWithClient } from "@/test/render";
import { CustomIngredientsPage } from "./custom-ingredients-page";

const oats = makeIngredient({ id: "g1", name: "Rolled oats" });
const granola = makeIngredient({
  id: "c1",
  name: "Granola",
  category: "grains_bread",
  is_custom: true,
  grams_per_piece: 40,
  nutrients: nutrients({ calories: 450, protein: 10, carbohydrates: 60, fat: 15, fibre: 7, iron: 3.5 }),
});
const kelp = makeIngredient({ id: "c2", name: "Kelp", category: "other", is_custom: true, nutrients: nutrients({ calories: 43 }) });
const page = (items: ReturnType<typeof makeIngredient>[], next_cursor: string | null = null) => json({ items, next_cursor });

describe("CustomIngredientsPage listing", () => {
  it("lists ingredients, marks mine as custom, and offers edit and delete on mine only", async () => {
    fakeApi({ "GET /ingredients": () => page([granola, oats, kelp]) });
    renderWithClient(<CustomIngredientsPage />);
    expect(await screen.findByText("Granola")).toBeInTheDocument();
    expect(screen.getAllByText("Custom")).toHaveLength(2);
    expect(screen.getByRole("button", { name: "Edit Granola" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Delete Kelp" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Edit Rolled oats" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Delete Rolled oats" })).not.toBeInTheDocument();
  });

  it("asks for the next page with the API's cursor", async () => {
    const fake = fakeApi({ "GET /ingredients": (req) => (req.search.get("cursor") === "c1" ? page([kelp]) : page([granola], "c1")) });
    renderWithClient(<CustomIngredientsPage />);
    await userEvent.click(await screen.findByRole("button", { name: "Load more" }));
    expect(await screen.findByText("Kelp")).toBeInTheDocument();
    expect(fake.callsTo("GET", "/ingredients")[1]?.search.get("cursor")).toBe("c1");
  });

  it("searches by name once typing settles, and filters by category", async () => {
    const fake = fakeApi({ "GET /ingredients": () => page([granola]) });
    renderWithClient(<CustomIngredientsPage />);
    await screen.findByText("Granola");
    await userEvent.type(screen.getByLabelText("Search ingredients"), "gran");
    await waitFor(() => expect(fake.calls.at(-1)?.search.get("q")).toBe("gran"));
    expect(fake.calls.map((c) => c.search.get("q"))).not.toContain("g");
    await userEvent.selectOptions(screen.getByLabelText("Filter by category"), "grains_bread");
    await waitFor(() => expect(fake.calls.at(-1)?.search.get("category")).toBe("grains_bread"));
  });

  it("shows only my custom ingredients on request, loading every page to find them", async () => {
    const fake = fakeApi({ "GET /ingredients": (req) => (req.search.get("cursor") === "c1" ? page([kelp, makeIngredient({ id: "g3", name: "Sugar" })]) : page([granola, oats], "c1")) });
    renderWithClient(<CustomIngredientsPage />);
    await screen.findByText("Rolled oats");
    await userEvent.click(screen.getByLabelText("Only my custom ingredients"));
    expect(await screen.findByText("Kelp")).toBeInTheDocument();
    expect(screen.getByText("Granola")).toBeInTheDocument();
    expect(screen.queryByText("Rolled oats")).not.toBeInTheDocument();
    expect(screen.queryByText("Sugar")).not.toBeInTheDocument();
    expect(fake.callsTo("GET", "/ingredients")).toHaveLength(2);
  });

  it("says so, with a way to make one, when there are no custom ingredients", async () => {
    fakeApi({ "GET /ingredients": () => page([oats]) });
    renderWithClient(<CustomIngredientsPage />);
    await screen.findByText("Rolled oats");
    await userEvent.click(screen.getByLabelText("Only my custom ingredients"));
    expect(await screen.findByText("You haven't created any custom ingredients yet.")).toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: "New custom ingredient" })).toHaveLength(2);
  });

  it("says when nothing matches a search", async () => {
    fakeApi({ "GET /ingredients": (req) => (req.search.get("q") ? page([]) : page([oats])) });
    renderWithClient(<CustomIngredientsPage />);
    await screen.findByText("Rolled oats");
    await userEvent.type(screen.getByLabelText("Search ingredients"), "zzz");
    expect(await screen.findByText("No ingredient matches.")).toBeInTheDocument();
  });

  it("offers a retry when the list cannot load", async () => {
    let fail = true;
    fakeApi({ "GET /ingredients": () => (fail ? problem(500, "internal_error") : page([granola])) });
    renderWithClient(<CustomIngredientsPage />);
    expect(await screen.findByText("Something went wrong. Try again.")).toBeInTheDocument();
    fail = false;
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByText("Granola")).toBeInTheDocument();
  });
});

describe("creating", () => {
  it("makes a custom ingredient with the shared dialog", async () => {
    const fake = fakeApi({ "GET /ingredients": () => page([oats]), "POST /ingredients": () => json(makeIngredient({ id: "c9", name: "Seitan", is_custom: true }), 201) });
    renderWithClient(<CustomIngredientsPage />);
    await userEvent.click(await screen.findByRole("button", { name: "New custom ingredient" }));
    const dialog = await screen.findByRole("dialog", { name: "New custom ingredient" });
    await userEvent.type(within(dialog).getByLabelText("Ingredient name"), "Seitan");
    await userEvent.click(within(dialog).getByRole("button", { name: "Create ingredient" }));
    await waitFor(() => expect(fake.callsTo("POST", "/ingredients")).toHaveLength(1));
    expect(fake.callsTo("POST", "/ingredients")[0]?.body).toEqual({ name: "Seitan", category: "other" });
  });
});

describe("editing", () => {
  async function openEdit() {
    await userEvent.click(await screen.findByRole("button", { name: "Edit Granola" }));
    return screen.findByRole("dialog", { name: "Edit custom ingredient" });
  }

  it("shows the ingredient as it is", async () => {
    fakeApi({ "GET /ingredients": () => page([granola]) });
    renderWithClient(<CustomIngredientsPage />);
    const dialog = await openEdit();
    expect(within(dialog).getByLabelText("Ingredient name")).toHaveValue("Granola");
    expect(within(dialog).getByLabelText("Category")).toHaveValue("grains_bread");
    expect(within(dialog).getByLabelText(/Calories per 100 g/)).toHaveValue("450");
    expect(within(dialog).getByLabelText(/Weight of one piece/)).toHaveValue("40");
    expect(within(dialog).getByLabelText(/Density/)).toHaveValue("");
  });

  it("saves with all 18 nutrients, so the ones the form does not show are not erased, and refreshes what depends on them", async () => {
    const fake = fakeApi({ "GET /ingredients": () => page([granola]), "PATCH /ingredients/:id": () => json({ ...granola, name: "Crunchy granola" }) });
    const { queryClient } = renderWithClient(<CustomIngredientsPage />);
    queryClient.setQueryData(["meals", "detail", "m1"], { id: "m1" });
    const dialog = await openEdit();
    await userEvent.clear(within(dialog).getByLabelText("Ingredient name"));
    await userEvent.type(within(dialog).getByLabelText("Ingredient name"), "Crunchy granola");
    await userEvent.click(within(dialog).getByRole("button", { name: "Save ingredient" }));

    await waitFor(() => expect(fake.callsTo("PATCH", "/ingredients/c1")).toHaveLength(1));
    const body = fake.callsTo("PATCH", "/ingredients/c1")[0]?.body as { name: string; nutrients: Record<string, number | null>; grams_per_piece: number | null; density_g_per_ml: number | null };
    expect(body.name).toBe("Crunchy granola");
    expect(Object.keys(body.nutrients)).toHaveLength(18);
    expect(body.nutrients).toMatchObject({ calories: 450, protein: 10, fibre: 7, iron: 3.5 });
    expect(body).toMatchObject({ grams_per_piece: 40, density_g_per_ml: null });
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(queryClient.getQueryState(["meals", "detail", "m1"])?.isInvalidated).toBe(true);
  });

  it("clears a blanked macro and a blanked weight per piece", async () => {
    const fake = fakeApi({ "GET /ingredients": () => page([granola]), "PATCH /ingredients/:id": () => json(granola) });
    renderWithClient(<CustomIngredientsPage />);
    const dialog = await openEdit();
    await userEvent.clear(within(dialog).getByLabelText(/Protein per 100 g/));
    await userEvent.clear(within(dialog).getByLabelText(/Weight of one piece/));
    await userEvent.click(within(dialog).getByRole("button", { name: "Save ingredient" }));
    await waitFor(() => expect(fake.callsTo("PATCH", "/ingredients/c1")).toHaveLength(1));
    expect(fake.callsTo("PATCH", "/ingredients/c1")[0]?.body).toMatchObject({ grams_per_piece: null, nutrients: { protein: null, calories: 450 } });
  });

  it("flags a mistake inline and sends nothing", async () => {
    const fake = fakeApi({ "GET /ingredients": () => page([granola]) });
    renderWithClient(<CustomIngredientsPage />);
    const dialog = await openEdit();
    fireEvent.change(within(dialog).getByLabelText(/Calories per 100 g/), { target: { value: "lots" } });
    await userEvent.click(within(dialog).getByRole("button", { name: "Save ingredient" }));
    expect(await within(dialog).findByText("Enter a number, for example 12.5.")).toBeInTheDocument();
    expect(fake.callsTo("PATCH", "/ingredients/c1")).toHaveLength(0);
  });

  it("explains that a meal still needs the weight when it is cleared, and stays open", async () => {
    fakeApi({ "GET /ingredients": () => page([granola]), "PATCH /ingredients/:id": () => problem(409, "unit_not_convertible") });
    renderWithClient(<CustomIngredientsPage />);
    const dialog = await openEdit();
    await userEvent.clear(within(dialog).getByLabelText(/Weight of one piece/));
    await userEvent.click(within(dialog).getByRole("button", { name: "Save ingredient" }));
    expect(await within(dialog).findByText(/can't be measured in that unit/)).toBeInTheDocument();
    expect(screen.getByRole("dialog", { name: "Edit custom ingredient" })).toBeInTheDocument();
  });
});

describe("deleting", () => {
  it("asks first, then deletes", async () => {
    const fake = fakeApi({ "GET /ingredients": () => page([kelp]), "DELETE /ingredients/:id": () => noContent() });
    renderWithClient(<CustomIngredientsPage />);
    await userEvent.click(await screen.findByRole("button", { name: "Delete Kelp" }));
    const dialog = await screen.findByRole("dialog", { name: "Delete this ingredient?" });
    expect(within(dialog).getByText(/Delete “Kelp”\?/)).toBeInTheDocument();
    expect(fake.callsTo("DELETE", "/ingredients/c2")).toHaveLength(0);
    await userEvent.click(within(dialog).getByRole("button", { name: "Delete ingredient" }));
    await waitFor(() => expect(fake.callsTo("DELETE", "/ingredients/c2")).toHaveLength(1));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  });

  it("says a meal still uses it, and stays open, when the API refuses", async () => {
    fakeApi({ "GET /ingredients": () => page([kelp]), "DELETE /ingredients/:id": () => problem(409, "ingredient_in_use") });
    renderWithClient(<CustomIngredientsPage />);
    await userEvent.click(await screen.findByRole("button", { name: "Delete Kelp" }));
    const dialog = await screen.findByRole("dialog", { name: "Delete this ingredient?" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Delete ingredient" }));
    expect(await within(dialog).findByText("A meal still uses this ingredient, so it can't be deleted.")).toBeInTheDocument();
    expect(screen.getByRole("dialog", { name: "Delete this ingredient?" })).toBeInTheDocument();
  });

  it("does nothing when the person backs out", async () => {
    const fake = fakeApi({ "GET /ingredients": () => page([kelp]) });
    renderWithClient(<CustomIngredientsPage />);
    await userEvent.click(await screen.findByRole("button", { name: "Delete Kelp" }));
    const dialog = await screen.findByRole("dialog", { name: "Delete this ingredient?" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Keep it" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(fake.callsTo("DELETE", "/ingredients/c2")).toHaveLength(0);
  });
});
