// @vitest-environment jsdom
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { fakeApi, json } from "@/test/fake-api";
import { makeIngredient } from "@/test/fixtures";
import { renderWithClient } from "@/test/render";
import { IngredientSearch } from "./ingredient-search";

const oats = makeIngredient();
const kale = makeIngredient({ id: "ing-kale", name: "Kale", category: "produce", is_custom: true });

function catalog() {
  return fakeApi({
    "GET /ingredients": (req) => {
      const q = (req.search.get("q") ?? "").toLowerCase();
      const items = [oats, kale].filter((i) => i.name.toLowerCase().includes(q));
      return json({ items, next_cursor: null });
    },
  });
}

// The category filter is a native <select> whose <option>s also have role "option": look only inside the results list.
const listOptions = () => within(screen.getByRole("listbox")).getAllByRole("option");

describe("IngredientSearch", () => {
  it("browses the catalogue on focus, narrows as you type, and selects with the keyboard", async () => {
    const onSelect = vi.fn();
    catalog();
    renderWithClient(<IngredientSearch onSelect={onSelect} />);
    const box = screen.getByRole("combobox", { name: "Add an ingredient" });

    await userEvent.click(box);
    expect(await screen.findByRole("option", { name: /Rolled oats/ })).toBeInTheDocument();
    expect(screen.getByRole("option", { name: /Kale/ })).toHaveTextContent("Custom");

    await userEvent.type(box, "ka");
    await waitFor(() => expect(listOptions()).toHaveLength(1));
    await userEvent.keyboard("{Enter}");

    expect(onSelect).toHaveBeenCalledWith(kale);
    expect(box).toHaveValue("");
  });

  it("moves through the results with the arrow keys and closes on Escape", async () => {
    const onSelect = vi.fn();
    catalog();
    renderWithClient(<IngredientSearch onSelect={onSelect} />);
    const box = screen.getByRole("combobox", { name: "Add an ingredient" });
    await userEvent.click(box);
    await screen.findByRole("option", { name: /Rolled oats/ });

    await userEvent.keyboard("{ArrowDown}");
    expect(listOptions()[1]).toHaveAttribute("aria-selected", "true");
    await userEvent.keyboard("{ArrowUp}{Enter}");
    expect(onSelect).toHaveBeenCalledWith(oats);

    await userEvent.click(box);
    await screen.findByRole("option", { name: /Rolled oats/ });
    await userEvent.keyboard("{Escape}");
    expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
  });

  it("asks the API only for the text that settled, so fast typing shows the latest results", async () => {
    const fake = catalog();
    renderWithClient(<IngredientSearch onSelect={vi.fn()} />);
    await userEvent.click(screen.getByRole("combobox", { name: "Add an ingredient" }));
    await userEvent.type(screen.getByRole("combobox", { name: "Add an ingredient" }), "kale");
    await waitFor(() => expect(listOptions()).toHaveLength(1));

    const asked = fake.calls.map((c) => c.search.get("q"));
    expect(asked).toContain("kale");
    expect(asked).not.toContain("k");
    expect(asked).not.toContain("ka");
  });

  it("filters by category", async () => {
    const fake = catalog();
    renderWithClient(<IngredientSearch onSelect={vi.fn()} />);
    await userEvent.selectOptions(screen.getByLabelText("Filter by category"), "produce");
    await userEvent.click(screen.getByRole("combobox", { name: "Add an ingredient" }));
    await waitFor(() => expect(fake.calls.at(-1)?.search.get("category")).toBe("produce"));
  });

  it("offers to create a custom ingredient when nothing matches, and selects the one it creates", async () => {
    const onSelect = vi.fn();
    const created = makeIngredient({ id: "ing-new", name: "Kelp", category: "other", is_custom: true });
    const fake = fakeApi({
      "GET /ingredients": () => json({ items: [], next_cursor: null }),
      "POST /ingredients": () => json(created, 201),
    });
    renderWithClient(<IngredientSearch onSelect={onSelect} />);

    await userEvent.type(screen.getByRole("combobox", { name: "Add an ingredient" }), "Kelp");
    expect(await screen.findByText(/No ingredient matches/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: /Create a custom ingredient/ }));

    const dialog = await screen.findByRole("dialog", { name: "New custom ingredient" });
    expect(within(dialog).getByLabelText("Ingredient name")).toHaveValue("Kelp");
    await userEvent.type(within(dialog).getByLabelText(/Calories per 100 g/), "43");
    await userEvent.click(within(dialog).getByRole("button", { name: "Create ingredient" }));

    await waitFor(() => expect(onSelect).toHaveBeenCalledWith(created));
    expect(fake.callsTo("POST", "/ingredients")[0]?.body).toEqual({ name: "Kelp", category: "other", nutrients: { calories: 43 } });
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  });

  it("keeps the dialog open and says what is wrong when the form is invalid", async () => {
    const fake = fakeApi({ "GET /ingredients": () => json({ items: [], next_cursor: null }) });
    renderWithClient(<IngredientSearch onSelect={vi.fn()} />);
    await userEvent.click(screen.getByRole("combobox", { name: "Add an ingredient" }));
    await userEvent.click(await screen.findByRole("button", { name: /Create a custom ingredient/ }));
    const dialog = await screen.findByRole("dialog");
    await userEvent.click(within(dialog).getByRole("button", { name: "Create ingredient" }));

    expect(within(dialog).getByText("Give the ingredient a name.")).toBeInTheDocument();
    expect(fake.callsTo("POST", "/ingredients")).toHaveLength(0);
  });
});
