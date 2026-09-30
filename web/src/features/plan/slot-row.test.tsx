// @vitest-environment jsdom
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { fakeApi, json } from "@/test/fake-api";
import { makeSummary } from "@/test/fixtures";
import { makeEntry } from "@/test/plan-fixtures";
import { renderWithClient } from "@/test/render";
import { SlotRow } from "./slot-row";

const meals = () =>
  fakeApi({ "GET /meals": () => json({ items: [makeSummary({ id: "m2", name: "Pasta bowl" }), makeSummary()], next_cursor: null }) });
const porridge = (portion = 2) => makeEntry({ slot: "breakfast", meal_id: "m1", meal_name: "Porridge", portion });

function show(slot: Parameters<typeof SlotRow>[0]["slot"], entries: ReturnType<typeof makeEntry>[]) {
  const onPick = vi.fn();
  const onClear = vi.fn();
  renderWithClient(<SlotRow slot={slot} entries={entries} onPick={onPick} onClear={onClear} />);
  return { onPick, onClear };
}

describe("SlotRow for breakfast, lunch and dinner", () => {
  it("offers to add a meal to an empty slot and hands over the pick with a portion of 1", async () => {
    meals();
    const { onPick } = show("breakfast", []);
    await userEvent.click(screen.getByRole("button", { name: "Add meal for breakfast" }));
    await userEvent.click(await screen.findByRole("button", { name: /Pasta bowl/ }));
    expect(onPick).toHaveBeenCalledWith(expect.objectContaining({ id: "m2", name: "Pasta bowl" }), 1);
  });

  it("shows the meal as a link with its portion", () => {
    show("breakfast", [porridge(1.5)]);
    expect(screen.getByRole("link", { name: "Porridge" })).toHaveAttribute("href", "/meals/m1");
    expect(screen.getByText("1.5 servings")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Add meal/ })).not.toBeInTheDocument();
  });

  it("changes the portion in half steps by asking for the same meal with the new portion", async () => {
    const { onPick } = show("breakfast", [porridge(2)]);
    await userEvent.click(screen.getByRole("button", { name: "Increase portion of Porridge" }));
    expect(onPick).toHaveBeenLastCalledWith({ id: "m1", name: "Porridge" }, 2.5);
    await userEvent.click(screen.getByRole("button", { name: "Decrease portion of Porridge" }));
    expect(onPick).toHaveBeenLastCalledWith({ id: "m1", name: "Porridge" }, 1.5);
  });

  it("cannot step the portion down to nothing, or past the API's limit", () => {
    show("breakfast", [porridge(0.5)]);
    expect(screen.getByRole("button", { name: "Decrease portion of Porridge" })).toBeDisabled();
  });

  it("cannot step the portion above 100", () => {
    show("lunch", [makeEntry({ slot: "lunch", meal_name: "Stew", portion: 100 })]);
    expect(screen.getByRole("button", { name: "Increase portion of Stew" })).toBeDisabled();
  });

  it("swaps the meal and keeps the current portion", async () => {
    meals();
    const { onPick } = show("breakfast", [porridge(2)]);
    await userEvent.click(screen.getByRole("button", { name: "Swap breakfast meal" }));
    await userEvent.click(await screen.findByRole("button", { name: /Pasta bowl/ }));
    expect(onPick).toHaveBeenCalledWith(expect.objectContaining({ id: "m2", name: "Pasta bowl" }), 2);
  });

  it("removes the meal", async () => {
    const { onClear } = show("breakfast", [porridge()]);
    await userEvent.click(screen.getByRole("button", { name: "Remove Porridge from breakfast" }));
    expect(onClear).toHaveBeenCalledTimes(1);
  });
});

describe("SlotRow for snacks", () => {
  const snacks = [makeEntry({ id: "s1", slot: "snack", meal_id: "m3", meal_name: "Apple", portion: 1 }), makeEntry({ id: "s2", slot: "snack", meal_id: "m4", meal_name: "Nuts", portion: 2 })];

  it("lists the snacks read-only, with no swap or portion controls, because the API cannot address one snack", () => {
    show("snack", snacks);
    expect(screen.getByRole("link", { name: "Apple" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Nuts" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /portion/i })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /swap/i })).not.toBeInTheDocument();
  });

  it("adds a snack with a portion of 1", async () => {
    meals();
    const { onPick } = show("snack", snacks);
    await userEvent.click(screen.getByRole("button", { name: "Add snack" }));
    await userEvent.click(await screen.findByRole("button", { name: /Pasta bowl/ }));
    expect(onPick).toHaveBeenCalledWith(expect.objectContaining({ id: "m2" }), 1);
  });

  it("clears all snacks only after a confirmation, and not when the person keeps them", async () => {
    const { onClear } = show("snack", snacks);
    await userEvent.click(screen.getByRole("button", { name: "Clear snacks" }));
    let dialog = await screen.findByRole("dialog", { name: "Clear all snacks?" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Keep them" }));
    expect(onClear).not.toHaveBeenCalled();

    await userEvent.click(screen.getByRole("button", { name: "Clear snacks" }));
    dialog = await screen.findByRole("dialog", { name: "Clear all snacks?" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Clear snacks" }));
    expect(onClear).toHaveBeenCalledTimes(1);
  });

  it("offers no clear when there are no snacks", () => {
    show("snack", []);
    expect(screen.queryByRole("button", { name: "Clear snacks" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Add snack" })).toBeInTheDocument();
  });
});
