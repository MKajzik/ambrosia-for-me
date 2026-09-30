// @vitest-environment jsdom
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { fakeApi, json } from "@/test/fake-api";
import { makeIngredient } from "@/test/fixtures";
import { renderWithClient } from "@/test/render";
import { QuickAdd } from "./quick-add";

function show() {
  const onAddText = vi.fn();
  const onAddIngredient = vi.fn();
  fakeApi({ "GET /ingredients": () => json({ items: [makeIngredient({ name: "Kale", category: "produce" })], next_cursor: null }) });
  renderWithClient(<QuickAdd onAddText={onAddText} onAddIngredient={onAddIngredient} />);
  return { onAddText, onAddIngredient };
}

describe("QuickAdd", () => {
  it("adds what was typed, trimmed, and clears the field so the next one can follow", async () => {
    const { onAddText } = show();
    await userEvent.type(screen.getByLabelText("Add an item"), "  Bin bags ");
    await userEvent.click(screen.getByRole("button", { name: "Add" }));
    expect(onAddText).toHaveBeenCalledWith("Bin bags");
    expect(screen.getByLabelText("Add an item")).toHaveValue("");
  });

  it("adds on Enter", async () => {
    const { onAddText } = show();
    await userEvent.type(screen.getByLabelText("Add an item"), "Milk{Enter}");
    expect(onAddText).toHaveBeenCalledWith("Milk");
  });

  it("says what to do, and adds nothing, for a blank entry", async () => {
    const { onAddText } = show();
    await userEvent.type(screen.getByLabelText("Add an item"), "   ");
    await userEvent.click(screen.getByRole("button", { name: "Add" }));
    expect(await screen.findByText("Type what to add.")).toBeInTheDocument();
    expect(onAddText).not.toHaveBeenCalled();
  });

  it("clears the reminder once something good is added", async () => {
    show();
    await userEvent.click(screen.getByRole("button", { name: "Add" }));
    expect(await screen.findByText("Type what to add.")).toBeInTheDocument();
    await userEvent.type(screen.getByLabelText("Add an item"), "Eggs{Enter}");
    await waitFor(() => expect(screen.queryByText("Type what to add.")).not.toBeInTheDocument());
  });

  it("adds an ingredient picked from the shared search", async () => {
    const { onAddIngredient } = show();
    await userEvent.click(screen.getByRole("combobox", { name: "Or add from ingredients" }));
    await userEvent.click(await screen.findByRole("option", { name: /Kale/ }));
    expect(onAddIngredient).toHaveBeenCalledWith(expect.objectContaining({ name: "Kale", category: "produce" }));
  });
});
