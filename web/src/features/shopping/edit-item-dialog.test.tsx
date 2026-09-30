// @vitest-environment jsdom
import { QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { fakeApi, json, noContent, problem } from "@/test/fake-api";
import { testQueryClient } from "@/test/render";
import { makeItem, makeList } from "@/test/shopping-fixtures";
import { EditItemDialog } from "./edit-item-dialog";
import { shoppingKeys, useShoppingList } from "./queries";

/** Renders the dialog for the item currently in the cached list, the way the screen does: the dialog follows the cache. */
function Live({ listId, itemId, onClose }: { listId: string; itemId: string; onClose: () => void }) {
  const list = useShoppingList(listId);
  const item = list.data?.items.find((i) => i.id === itemId) ?? null;
  return <EditItemDialog key={item ? `${item.id}` : "none"} item={item} listId={listId} onClose={onClose} />;
}

function show(routes: Parameters<typeof fakeApi>[0] = {}, item = makeItem({ id: "i1", name: "Milk", quantity: 2, unit: "piece", category: "dairy_eggs", version: 1 })) {
  const list = makeList({ items: [item] });
  const fake = fakeApi({ "GET /shopping-lists/:id": () => json(list), ...routes });
  const queryClient = testQueryClient();
  queryClient.setQueryData(shoppingKeys.detail("l1"), list);
  const onClose = vi.fn();
  render(
    <QueryClientProvider client={queryClient}>
      <Live listId="l1" itemId="i1" onClose={onClose} />
    </QueryClientProvider>,
  );
  return { fake, onClose, queryClient, dialog: () => screen.getByRole("dialog", { name: "Edit item" }) };
}

describe("EditItemDialog", () => {
  it("shows the item as it is", async () => {
    const { dialog } = show();
    expect(await screen.findByRole("dialog", { name: "Edit item" })).toBeInTheDocument();
    expect(within(dialog()).getByLabelText("Name")).toHaveValue("Milk");
    expect(within(dialog()).getByLabelText("Quantity")).toHaveValue("2");
    expect(within(dialog()).getByLabelText("Unit")).toHaveValue("piece");
    expect(within(dialog()).getByLabelText("Category")).toHaveValue("dairy_eggs");
  });

  it("saves the four fields together with the item's version, then closes", async () => {
    const { fake, onClose, dialog } = show({ "PATCH /shopping-lists/:id/items/:item": () => json(makeItem({ id: "i1", name: "Oat milk", quantity: 1.5, unit: "ml", version: 2 })) });
    await screen.findByRole("dialog", { name: "Edit item" });
    await userEvent.clear(within(dialog()).getByLabelText("Name"));
    await userEvent.type(within(dialog()).getByLabelText("Name"), "Oat milk");
    await userEvent.clear(within(dialog()).getByLabelText("Quantity"));
    await userEvent.type(within(dialog()).getByLabelText("Quantity"), "1,5");
    await userEvent.selectOptions(within(dialog()).getByLabelText("Unit"), "ml");
    await userEvent.click(within(dialog()).getByRole("button", { name: "Save" }));

    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(fake.callsTo("PATCH", "/shopping-lists/l1/items/i1")[0]?.body).toEqual({ version: 1, name: "Oat milk", quantity: 1.5, unit: "ml", category: "dairy_eggs" });
  });

  it("clears a quantity and a unit that were blanked, sending null rather than zero", async () => {
    const { fake, dialog } = show({ "PATCH /shopping-lists/:id/items/:item": () => json(makeItem({ id: "i1", version: 2 })) });
    await screen.findByRole("dialog", { name: "Edit item" });
    await userEvent.clear(within(dialog()).getByLabelText("Quantity"));
    await userEvent.selectOptions(within(dialog()).getByLabelText("Unit"), "");
    await userEvent.click(within(dialog()).getByRole("button", { name: "Save" }));
    await waitFor(() => expect(fake.callsTo("PATCH", "/shopping-lists/l1/items/i1")).toHaveLength(1));
    expect(fake.callsTo("PATCH", "/shopping-lists/l1/items/i1")[0]?.body).toEqual({ version: 1, name: "Milk", quantity: null, unit: null, category: "dairy_eggs" });
  });

  it("sends nothing and just closes when nothing changed", async () => {
    const { fake, onClose, dialog } = show();
    await screen.findByRole("dialog", { name: "Edit item" });
    await userEvent.click(within(dialog()).getByRole("button", { name: "Save" }));
    expect(fake.callsTo("PATCH", "/shopping-lists/l1/items/i1")).toHaveLength(0);
    expect(onClose).toHaveBeenCalled();
  });

  it.each([
    ["a blank name", "Name", "", "Give the item a name."],
    ["text for a quantity", "Quantity", "lots", "Enter a number, for example 2 or 1.5."],
    ["a quantity of zero", "Quantity", "0", "The quantity must be more than 0 and at most 100000."],
    ["a huge quantity", "Quantity", "100001", "The quantity must be more than 0 and at most 100000."],
  ])("refuses %s inline and sends nothing", async (_what, label, value, message) => {
    const { fake, dialog } = show();
    await screen.findByRole("dialog", { name: "Edit item" });
    await userEvent.clear(within(dialog()).getByLabelText(label));
    if (value) await userEvent.type(within(dialog()).getByLabelText(label), value);
    await userEvent.click(within(dialog()).getByRole("button", { name: "Save" }));
    expect(await within(dialog()).findByText(message)).toBeInTheDocument();
    expect(fake.callsTo("PATCH", "/shopping-lists/l1/items/i1")).toHaveLength(0);
  });

  it("shows the other person's version and asks to review when the item was changed under it, then saves against the new version", async () => {
    const theirs = makeItem({ id: "i1", name: "Bananas", quantity: 6, unit: "piece", category: "produce", version: 3 });
    let attempts = 0;
    const { fake, onClose, dialog } = show({
      "PATCH /shopping-lists/:id/items/:item": () => (++attempts === 1 ? problem(409, "version_conflict", { current: theirs }) : json(makeItem({ ...theirs, name: "Bananas!", version: 4 }))),
    });
    await screen.findByRole("dialog", { name: "Edit item" });
    await userEvent.clear(within(dialog()).getByLabelText("Name"));
    await userEvent.type(within(dialog()).getByLabelText("Name"), "Oat milk");
    await userEvent.click(within(dialog()).getByRole("button", { name: "Save" }));

    expect(await within(dialog()).findByText("Someone else changed this item. Review it and save again.")).toBeInTheDocument();
    await waitFor(() => expect(within(dialog()).getByLabelText("Name")).toHaveValue("Bananas"));
    expect(within(dialog()).getByLabelText("Quantity")).toHaveValue("6");
    expect(onClose).not.toHaveBeenCalled();

    await userEvent.type(within(dialog()).getByLabelText("Name"), "!");
    await userEvent.click(within(dialog()).getByRole("button", { name: "Save" }));
    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(fake.callsTo("PATCH", "/shopping-lists/l1/items/i1")[1]?.body).toMatchObject({ version: 3, name: "Bananas!" });
  });

  it("keeps what is being typed when someone else's change arrives, and saves against the version it was opened on, so that change is a conflict and not an overwrite", async () => {
    const theirs = makeItem({ id: "i1", name: "Milk 1L", quantity: 2, unit: "piece", category: "dairy_eggs", version: 2 });
    const { fake, queryClient, dialog } = show({ "PATCH /shopping-lists/:id/items/:item": () => problem(409, "version_conflict", { current: theirs }) });
    await screen.findByRole("dialog", { name: "Edit item" });
    await userEvent.clear(within(dialog()).getByLabelText("Name"));
    await userEvent.type(within(dialog()).getByLabelText("Name"), "Oat milk");

    // The partner's edit arrives over the stream and lands in the cache while the form is open.
    queryClient.setQueryData(shoppingKeys.detail("l1"), makeList({ items: [theirs] }));
    await waitFor(() => expect(queryClient.getQueryData(shoppingKeys.detail("l1"))).toMatchObject({ items: [{ version: 2 }] }));
    expect(within(dialog()).getByLabelText("Name")).toHaveValue("Oat milk");

    await userEvent.click(within(dialog()).getByRole("button", { name: "Save" }));
    expect(await within(dialog()).findByText("Someone else changed this item. Review it and save again.")).toBeInTheDocument();
    await waitFor(() => expect(within(dialog()).getByLabelText("Name")).toHaveValue("Milk 1L"));
    expect(fake.callsTo("PATCH", "/shopping-lists/l1/items/i1")[0]?.body).toMatchObject({ version: 1, name: "Oat milk" });
  });

  it("explains any other failure and keeps what was typed", async () => {
    const { onClose, dialog } = show({ "PATCH /shopping-lists/:id/items/:item": () => problem(500, "internal_error") });
    await screen.findByRole("dialog", { name: "Edit item" });
    await userEvent.type(within(dialog()).getByLabelText("Name"), " 2");
    await userEvent.click(within(dialog()).getByRole("button", { name: "Save" }));
    await waitFor(() => expect(within(dialog()).getByRole("button", { name: "Save" })).toBeEnabled());
    expect(onClose).not.toHaveBeenCalled();
    expect(within(dialog()).getByLabelText("Name")).toHaveValue("Milk 2");
  });

  it("removes the item", async () => {
    const { fake, onClose, dialog } = show({ "DELETE /shopping-lists/:id/items/:item": () => noContent() });
    await screen.findByRole("dialog", { name: "Edit item" });
    await userEvent.click(within(dialog()).getByRole("button", { name: "Remove item" }));
    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(fake.callsTo("DELETE", "/shopping-lists/l1/items/i1")).toHaveLength(1);
  });

  it("is closed when there is no item to edit", () => {
    render(
      <QueryClientProvider client={testQueryClient()}>
        <EditItemDialog item={null} listId="l1" onClose={() => undefined} />
      </QueryClientProvider>,
    );
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });
});
