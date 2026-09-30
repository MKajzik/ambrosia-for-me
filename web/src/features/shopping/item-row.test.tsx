// @vitest-environment jsdom
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { makeItem } from "@/test/shopping-fixtures";
import { ItemRow } from "./item-row";

function show(item = makeItem(), checkedByName: string | null = null) {
  const onToggle = vi.fn();
  const onEdit = vi.fn();
  render(
    <ul>
      <ItemRow item={item} checkedByName={checkedByName} onToggle={onToggle} onEdit={onEdit} />
    </ul>,
  );
  return { onToggle, onEdit };
}

describe("ItemRow", () => {
  it("shows the name and quantity in the checkbox's label, and toggles", async () => {
    const item = makeItem({ name: "Flour", quantity: 500, unit: "g" });
    const { onToggle } = show(item);
    const box = screen.getByRole("checkbox", { name: /Flour/ });
    expect(box).not.toBeChecked();
    expect(screen.getByText("500 g")).toBeInTheDocument();
    await userEvent.click(box);
    expect(onToggle).toHaveBeenCalledWith(item);
  });

  it("shows a checked item struck through, and who checked it when that is someone else", () => {
    show(makeItem({ name: "Milk", checked: true, checked_by: "u2" }), "Sam");
    expect(screen.getByRole("checkbox", { name: /Milk/ })).toBeChecked();
    expect(screen.getByText("Milk")).toHaveClass("line-through");
    expect(screen.getByText("Checked by Sam")).toBeInTheDocument();
  });

  it("does not attribute a check to anyone unless told to", () => {
    show(makeItem({ checked: true, checked_by: "u1" }), null);
    expect(screen.queryByText(/Checked by/)).not.toBeInTheDocument();
  });

  it("opens the editor from its button", async () => {
    const item = makeItem({ name: "Milk" });
    const { onEdit } = show(item);
    await userEvent.click(screen.getByRole("button", { name: "Edit Milk" }));
    expect(onEdit).toHaveBeenCalledWith(item);
  });

  it("cannot be checked or edited before the server has answered for it", () => {
    show(makeItem({ id: "optimistic:1", name: "Bread" }));
    expect(screen.getByRole("checkbox", { name: /Bread/ })).toBeDisabled();
    expect(screen.queryByRole("button", { name: "Edit Bread" })).not.toBeInTheDocument();
  });
});
