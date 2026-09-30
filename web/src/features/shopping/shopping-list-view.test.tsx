// @vitest-environment jsdom
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { FakeEventSource } from "@/test/fake-event-source";
import { fakeApi, json, problem } from "@/test/fake-api";
import { makeIngredient, partnership } from "@/test/fixtures";
import { renderWithClient } from "@/test/render";
import { makeItem, makeList, makeUser } from "@/test/shopping-fixtures";
import type { ShoppingList } from "./items";
import { ShoppingListView } from "./shopping-list-view";

const toastError = vi.fn();
vi.mock("sonner", () => ({ toast: { error: (message: string) => toastError(message), success: vi.fn() } }));
vi.mock("next/navigation", () => ({ useRouter: () => ({ replace: vi.fn(), push: vi.fn() }) }));

beforeEach(() => FakeEventSource.install());
afterEach(() => toastError.mockReset());

const items = () => [
  makeItem({ id: "i1", name: "Milk", category: "dairy_eggs", position: 0 }),
  makeItem({ id: "i2", name: "Apples", category: "produce", position: 1, quantity: 6, unit: "piece" }),
  makeItem({ id: "i3", name: "Carrots", category: "produce", position: 2, checked: true, checked_by: "u1" }),
];
const noPartner = { "GET /partner": () => problem(404, "partner_not_linked") };
const me = { "GET /me": () => json(makeUser()) };

function show(initial: ShoppingList = makeList({ items: items() }), routes: Parameters<typeof fakeApi>[0] = {}) {
  let server = initial;
  const fake = fakeApi({ "GET /shopping-lists/:id": () => json(server), ...me, ...noPartner, ...routes });
  const view = renderWithClient(<ShoppingListView id="l1" />);
  return { fake, serve: (next: ShoppingList) => void (server = next), ...view };
}

describe("the list", () => {
  it("groups items by aisle, with what is left to buy first, and counts what is checked", async () => {
    show();
    const produce = await screen.findByRole("region", { name: "Produce" });
    expect(within(produce).getAllByRole("checkbox").map((c) => c.getAttribute("aria-label") ?? c.closest("label")?.textContent)).toEqual(["Apples6 pieces", "Carrots"]);
    expect(screen.getByRole("region", { name: "Dairy and eggs" })).toBeInTheDocument();
    expect(screen.getByText("1 of 3 checked")).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Weekly shop" })).toBeInTheDocument();
  });

  it("names the plan range a generated list came from", async () => {
    show(makeList({ items: items(), source_from: "2026-09-28", source_to: "2026-10-04" }));
    expect(await screen.findByText(/Sep 28 – Oct 4/)).toBeInTheDocument();
  });

  it("says so when there is nothing on the list yet", async () => {
    show(makeList());
    expect(await screen.findByText(/No items yet/)).toBeInTheDocument();
  });

  it("offers a retry when the list cannot be loaded for another reason", async () => {
    let fail = true;
    fakeApi({ "GET /shopping-lists/:id": () => (fail ? problem(500, "internal_error") : json(makeList({ items: items() }))), ...me, ...noPartner });
    renderWithClient(<ShoppingListView id="l1" />);
    expect(await screen.findByText("Something went wrong. Try again.")).toBeInTheDocument();
    fail = false;
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByRole("region", { name: "Produce" })).toBeInTheDocument();
  });
});

describe("checking items off", () => {
  it("ticks at once and tells the API only that it is checked, with no version", async () => {
    const { fake } = show(undefined, {
      "PATCH /shopping-lists/:id/items/:item": () => json(makeItem({ id: "i2", name: "Apples", category: "produce", position: 1, quantity: 6, unit: "piece", checked: true, checked_by: "u1", version: 2 })),
    });
    await userEvent.click(await screen.findByRole("checkbox", { name: /Apples/ }));
    await waitFor(() => expect(screen.getByRole("checkbox", { name: /Apples/ })).toBeChecked());
    expect(fake.callsTo("PATCH", "/shopping-lists/l1/items/i2")[0]?.body).toEqual({ checked: true });
    expect(screen.getByText("2 of 3 checked")).toBeInTheDocument();
  });

  it("puts the tick back and says why when the server refuses", async () => {
    show(undefined, { "PATCH /shopping-lists/:id/items/:item": () => problem(500, "internal_error") });
    await userEvent.click(await screen.findByRole("checkbox", { name: /Apples/ }));
    await waitFor(() => expect(toastError).toHaveBeenCalledWith("Something went wrong. Try again."));
    expect(screen.getByRole("checkbox", { name: /Apples/ })).not.toBeChecked();
    expect(screen.getByText("1 of 3 checked")).toBeInTheDocument();
  });

  it("ends on the last tap when a checkbox is tapped twice quickly", async () => {
    const { fake } = show(undefined, { "PATCH /shopping-lists/:id/items/:item": (req) => json(makeItem({ id: "i2", name: "Apples", category: "produce", position: 1, checked: (req.body as { checked: boolean }).checked, version: 2 })) });
    const box = await screen.findByRole("checkbox", { name: /Apples/ });
    await userEvent.click(box);
    await userEvent.click(box);
    await waitFor(() => expect(fake.callsTo("PATCH", "/shopping-lists/l1/items/i2")).toHaveLength(2));
    expect(fake.callsTo("PATCH", "/shopping-lists/l1/items/i2").map((c) => c.body)).toEqual([{ checked: true }, { checked: false }]);
    await waitFor(() => expect(screen.getByRole("checkbox", { name: /Apples/ })).not.toBeChecked());
  });
});

describe("quick-add", () => {
  it("shows the new item at once under Other and sends just its name", async () => {
    let release!: () => void;
    const { fake } = show(undefined, {
      "POST /shopping-lists/:id/items": () =>
        new Promise<Response>((resolve) => {
          release = () => resolve(json(makeItem({ id: "new1", name: "Bin bags", category: "other", position: 3 }), 201));
        }),
    });
    await userEvent.type(await screen.findByLabelText("Add an item"), "Bin bags{Enter}");
    const other = await screen.findByRole("region", { name: "Other" });
    const box = within(other).getByRole("checkbox", { name: /Bin bags/ });
    expect(box).toBeDisabled();
    expect(within(other).queryByRole("button", { name: "Edit Bin bags" })).not.toBeInTheDocument();
    expect(fake.callsTo("POST", "/shopping-lists/l1/items")[0]?.body).toEqual({ name: "Bin bags" });

    release();
    await waitFor(() => expect(within(screen.getByRole("region", { name: "Other" })).getByRole("checkbox", { name: /Bin bags/ })).toBeEnabled());
    expect(within(screen.getByRole("region", { name: "Other" })).getByRole("button", { name: "Edit Bin bags" })).toBeInTheDocument();
  });

  it("adds an ingredient by id, so it lands in that ingredient's aisle", async () => {
    const fake = show(undefined, {
      "GET /ingredients": () => json({ items: [makeIngredient({ id: "ing-kale", name: "Kale", category: "produce" })], next_cursor: null }),
      "POST /shopping-lists/:id/items": () => json(makeItem({ id: "k1", name: "Kale", category: "produce", position: 3, ingredient_id: "ing-kale" }), 201),
    }).fake;
    await userEvent.click(await screen.findByRole("combobox", { name: "Or add from ingredients" }));
    await userEvent.click(await screen.findByRole("option", { name: /Kale/ }));
    await waitFor(() => expect(fake.callsTo("POST", "/shopping-lists/l1/items")).toHaveLength(1));
    expect(fake.callsTo("POST", "/shopping-lists/l1/items")[0]?.body).toEqual({ name: "Kale", ingredient_id: "ing-kale" });
    expect(within(screen.getByRole("region", { name: "Produce" })).getByRole("checkbox", { name: /Kale/ })).toBeInTheDocument();
  });

  it("takes the item back out and says why when the server refuses it", async () => {
    show(undefined, { "POST /shopping-lists/:id/items": () => problem(404, "not_found") });
    await userEvent.type(await screen.findByLabelText("Add an item"), "Bin bags{Enter}");
    await waitFor(() => expect(toastError).toHaveBeenCalled());
    expect(screen.queryByRole("checkbox", { name: /Bin bags/ })).not.toBeInTheDocument();
  });
});

describe("a list shared with the partner", () => {
  const sharedByMe = () => makeList({ shared_with_partner: true, items: [makeItem({ id: "i1", name: "Milk", checked: true, checked_by: "u2" }), makeItem({ id: "i2", name: "Eggs", position: 1, checked: true, checked_by: "u1" })] });

  it("badges my shared list with the partner's name and attributes their check-offs, and mine to nobody", async () => {
    show(sharedByMe(), { "GET /partner": () => json(partnership()) });
    expect(await screen.findByText("Shared with Sam")).toBeInTheDocument();
    expect(screen.getByText("Checked by Sam")).toBeInTheDocument();
    expect(screen.getAllByText(/Checked by/)).toHaveLength(1);
    expect(screen.getByRole("button", { name: "List settings" })).toBeInTheDocument();
  });

  it("shows the partner's list as theirs, with no settings but with everything else", async () => {
    show(makeList({ is_owner: false, shared_with_partner: true, items: items() }), { "GET /partner": () => json(partnership()) });
    expect(await screen.findByText("Sam’s list")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "List settings" })).not.toBeInTheDocument();
    expect(screen.getByLabelText("Add an item")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Edit Apples" })).toBeInTheDocument();
  });

  it("opens the settings for the owner", async () => {
    show(sharedByMe(), { "GET /partner": () => json(partnership()) });
    await userEvent.click(await screen.findByRole("button", { name: "List settings" }));
    expect(await screen.findByRole("dialog", { name: "List settings" })).toBeInTheDocument();
  });
});

describe("editing an item", () => {
  it("opens the editor for the item and follows a saved change", async () => {
    show(undefined, { "PATCH /shopping-lists/:id/items/:item": () => json(makeItem({ id: "i1", name: "Oat milk", category: "dairy_eggs", version: 2 })) });
    await userEvent.click(await screen.findByRole("button", { name: "Edit Milk" }));
    const dialog = await screen.findByRole("dialog", { name: "Edit item" });
    await userEvent.clear(within(dialog).getByLabelText("Name"));
    await userEvent.type(within(dialog).getByLabelText("Name"), "Oat milk");
    await userEvent.click(within(dialog).getByRole("button", { name: "Save" }));
    expect(await screen.findByRole("checkbox", { name: /Oat milk/ })).toBeInTheDocument();
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  });

  it("closes the editor when the item is removed by someone else while it is open", async () => {
    const { serve } = show();
    await userEvent.click(await screen.findByRole("button", { name: "Edit Milk" }));
    await screen.findByRole("dialog", { name: "Edit item" });
    serve(makeList({ items: items().slice(1) }));
    FakeEventSource.last!.message("item_deleted", { type: "item_deleted", list_id: "l1", item_id: "i1", version: 1 });
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  });
});

describe("live updates", () => {
  it("opens the list's stream", async () => {
    show();
    await screen.findByRole("region", { name: "Produce" });
    expect(FakeEventSource.last?.url).toBe("/api/shopping-lists/l1/events");
  });

  it("shows what the partner ticked without a reload", async () => {
    const { serve } = show(makeList({ shared_with_partner: true, items: items() }), { "GET /partner": () => json(partnership()) });
    await screen.findByRole("region", { name: "Produce" });
    serve(makeList({ shared_with_partner: true, items: [makeItem({ id: "i1", name: "Milk", category: "dairy_eggs", checked: true, checked_by: "u2", version: 2 }), ...items().slice(1)] }));
    FakeEventSource.last!.message("item_changed", { type: "item_changed", list_id: "l1", item_id: "i1", version: 2 });
    await waitFor(() => expect(screen.getByRole("checkbox", { name: /Milk/ })).toBeChecked());
    expect(screen.getByText("Checked by Sam")).toBeInTheDocument();
  });

  it("says the list was deleted, with a way back, when the stream says so", async () => {
    show();
    await screen.findByRole("region", { name: "Produce" });
    FakeEventSource.last!.message("list_deleted", { type: "list_deleted", list_id: "l1" });
    expect(await screen.findByText("This list was deleted.")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /Shopping/ })).toHaveAttribute("href", "/shopping");
    expect(screen.queryByRole("region", { name: "Produce" })).not.toBeInTheDocument();
  });

  it("shows the list as unavailable, not stale items or a retry, when access is lost while it is open", async () => {
    let gone = false;
    fakeApi({ "GET /shopping-lists/:id": () => (gone ? problem(404, "not_found") : json(makeList({ shared_with_partner: true, is_owner: false, items: items() }))), ...me, "GET /partner": () => json(partnership()) });
    renderWithClient(<ShoppingListView id="l1" />);
    await screen.findByRole("region", { name: "Produce" });

    gone = true;
    FakeEventSource.last!.fail(true);
    expect(await screen.findByText("That list isn't available. It may have been deleted, or it is no longer shared with you.")).toBeInTheDocument();
    expect(screen.queryByRole("region", { name: "Produce" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Try again" })).not.toBeInTheDocument();
  });

  it("is unavailable at once when the list was never visible", async () => {
    fakeApi({ "GET /shopping-lists/:id": () => problem(404, "not_found"), ...me, ...noPartner });
    renderWithClient(<ShoppingListView id="nope" />);
    expect(await screen.findByText("That list isn't available. It may have been deleted, or it is no longer shared with you.")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /Shopping/ })).toHaveAttribute("href", "/shopping");
  });
});
