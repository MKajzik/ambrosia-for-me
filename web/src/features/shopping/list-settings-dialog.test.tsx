// @vitest-environment jsdom
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { fakeApi, json, noContent, problem } from "@/test/fake-api";
import { partnership } from "@/test/fixtures";
import { renderWithClient } from "@/test/render";
import { makeList } from "@/test/shopping-fixtures";
import { ListSettingsDialog } from "./list-settings-dialog";

const replace = vi.fn();
vi.mock("next/navigation", () => ({ useRouter: () => ({ replace: (url: string) => replace(url), push: vi.fn() }) }));
afterEach(() => replace.mockReset());

const noPartner = { "GET /partner": () => problem(404, "partner_not_linked") };
const generated = () => makeList({ name: "Week shop", source_from: "2026-09-28", source_to: "2026-10-04" });

function open(list = makeList(), routes: Parameters<typeof fakeApi>[0] = noPartner) {
  const fake = fakeApi(routes);
  const onOpenChange = vi.fn();
  const view = renderWithClient(<ListSettingsDialog list={list} open onOpenChange={onOpenChange} />);
  return { fake, onOpenChange, ...view, dialog: () => screen.getByRole("dialog", { name: "List settings" }) };
}

describe("rename and share", () => {
  it("renames the list, sending only the name", async () => {
    const { fake, onOpenChange, dialog } = open(makeList(), { ...noPartner, "PATCH /shopping-lists/:id": () => json(makeList({ name: "Party" })) });
    await userEvent.clear(within(dialog()).getByLabelText("Name"));
    await userEvent.type(within(dialog()).getByLabelText("Name"), "Party");
    await userEvent.click(within(dialog()).getByRole("button", { name: "Save" }));
    await waitFor(() => expect(fake.callsTo("PATCH", "/shopping-lists/l1")).toHaveLength(1));
    expect(fake.callsTo("PATCH", "/shopping-lists/l1")[0]?.body).toEqual({ name: "Party" });
    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
  });

  it("shares with the partner while linked, sending only the choice", async () => {
    const { fake, dialog } = open(makeList(), { "GET /partner": () => json(partnership()), "PATCH /shopping-lists/:id": () => json(makeList({ shared_with_partner: true })) });
    await userEvent.click(await within(dialog()).findByLabelText("Share with my partner"));
    await userEvent.click(within(dialog()).getByRole("button", { name: "Save" }));
    await waitFor(() => expect(fake.callsTo("PATCH", "/shopping-lists/l1")).toHaveLength(1));
    expect(fake.callsTo("PATCH", "/shopping-lists/l1")[0]?.body).toEqual({ shared_with_partner: true });
  });

  it("hides the sharing choice when there is no partner and the list is not shared", async () => {
    const { queryClient, dialog } = open();
    await waitFor(() => expect(queryClient.getQueryState(["partner"])?.status).toBe("success"));
    expect(within(dialog()).queryByLabelText("Share with my partner")).not.toBeInTheDocument();
  });

  it("lets a shared list be unshared even after the partner is gone", async () => {
    const { dialog } = open(makeList({ shared_with_partner: true }));
    expect(await within(dialog()).findByLabelText("Share with my partner")).toBeChecked();
  });

  it("sends nothing and just closes when nothing changed", async () => {
    const { fake, onOpenChange, dialog } = open();
    await userEvent.click(within(dialog()).getByRole("button", { name: "Save" }));
    expect(fake.callsTo("PATCH", "/shopping-lists/l1")).toHaveLength(0);
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("asks for a name and sends nothing without one", async () => {
    const { fake, dialog } = open();
    await userEvent.clear(within(dialog()).getByLabelText("Name"));
    await userEvent.click(within(dialog()).getByRole("button", { name: "Save" }));
    expect(await within(dialog()).findByText("Give the list a name.")).toBeInTheDocument();
    expect(fake.callsTo("PATCH", "/shopping-lists/l1")).toHaveLength(0);
  });
});

describe("regenerate", () => {
  it("is not offered for a list built by hand", () => {
    const { dialog } = open();
    expect(within(dialog()).queryByRole("button", { name: "Regenerate from plan" })).not.toBeInTheDocument();
  });

  it("says what it replaces and what it keeps before doing anything, and does nothing if the person backs out", async () => {
    const { fake, dialog } = open(generated());
    expect(within(dialog()).getByText(/Built from your plan for Sep 28 – Oct 4/)).toBeInTheDocument();
    await userEvent.click(within(dialog()).getByRole("button", { name: "Regenerate from plan" }));
    expect(within(dialog()).getByText(/replaces the items generated from your plan/i)).toBeInTheDocument();
    expect(within(dialog()).getByText(/items you added yourself stay/i)).toBeInTheDocument();
    expect(within(dialog()).getByText(/checks on generated items are lost/i)).toBeInTheDocument();
    await userEvent.click(within(dialog()).getByRole("button", { name: "Cancel" }));
    expect(fake.callsTo("POST", "/shopping-lists/generate")).toHaveLength(0);
    expect(within(dialog()).getByRole("button", { name: "Regenerate from plan" })).toBeInTheDocument();
  });

  it("regenerates from the list's own dates, for that list", async () => {
    const { fake, onOpenChange, dialog } = open(generated(), { ...noPartner, "POST /shopping-lists/generate": () => json(generated(), 200) });
    await userEvent.click(within(dialog()).getByRole("button", { name: "Regenerate from plan" }));
    await userEvent.click(within(dialog()).getByRole("button", { name: "Regenerate" }));
    await waitFor(() => expect(fake.callsTo("POST", "/shopping-lists/generate")).toHaveLength(1));
    expect(fake.callsTo("POST", "/shopping-lists/generate")[0]?.body).toEqual({ from: "2026-09-28", to: "2026-10-04", list_id: "l1" });
    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
  });

  it("says why it failed and stays open", async () => {
    const { onOpenChange, dialog } = open(generated(), { ...noPartner, "POST /shopping-lists/generate": () => problem(404, "not_found") });
    await userEvent.click(within(dialog()).getByRole("button", { name: "Regenerate from plan" }));
    await userEvent.click(within(dialog()).getByRole("button", { name: "Regenerate" }));
    expect(await within(dialog()).findByText("That isn't available. It may have been removed, or it isn't shared with you.")).toBeInTheDocument();
    expect(onOpenChange).not.toHaveBeenCalledWith(false);
  });
});

describe("delete", () => {
  it("asks first, naming the list, and deletes only when confirmed, then returns to the lists", async () => {
    const { fake, dialog } = open(makeList({ name: "Party" }), { ...noPartner, "DELETE /shopping-lists/:id": () => noContent() });
    await userEvent.click(within(dialog()).getByRole("button", { name: "Delete list" }));
    expect(within(dialog()).getByText(/Delete “Party” and all its items\? This can't be undone\./)).toBeInTheDocument();
    expect(fake.callsTo("DELETE", "/shopping-lists/l1")).toHaveLength(0);
    await userEvent.click(within(dialog()).getByRole("button", { name: "Delete list" }));
    await waitFor(() => expect(replace).toHaveBeenCalledWith("/shopping"));
    expect(fake.callsTo("DELETE", "/shopping-lists/l1")).toHaveLength(1);
  });

  it("does nothing when the person backs out", async () => {
    const { fake, dialog } = open();
    await userEvent.click(within(dialog()).getByRole("button", { name: "Delete list" }));
    await userEvent.click(within(dialog()).getByRole("button", { name: "Cancel" }));
    expect(fake.callsTo("DELETE", "/shopping-lists/l1")).toHaveLength(0);
    expect(replace).not.toHaveBeenCalled();
  });

  it("stays put and says why when the delete fails", async () => {
    const { dialog } = open(makeList(), { ...noPartner, "DELETE /shopping-lists/:id": () => problem(500, "internal_error") });
    await userEvent.click(within(dialog()).getByRole("button", { name: "Delete list" }));
    await userEvent.click(within(dialog()).getByRole("button", { name: "Delete list" }));
    expect(await within(dialog()).findByText("Something went wrong. Try again.")).toBeInTheDocument();
    expect(replace).not.toHaveBeenCalled();
  });
});
