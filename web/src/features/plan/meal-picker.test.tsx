// @vitest-environment jsdom
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { fakeApi, json, problem } from "@/test/fake-api";
import { makeSummary } from "@/test/fixtures";
import { renderWithClient } from "@/test/render";
import { MealPicker } from "./meal-picker";

const twoPages = () =>
  fakeApi({
    "GET /meals": (req) =>
      req.search.get("cursor") === "c1"
        ? json({ items: [makeSummary({ id: "m2", name: "Pasta bowl" })], next_cursor: null })
        : json({ items: [makeSummary()], next_cursor: "c1" }),
  });

function open(onPick = vi.fn(), onOpenChange = vi.fn()) {
  renderWithClient(<MealPicker open onOpenChange={onOpenChange} title="Choose breakfast" onPick={onPick} />);
  return { onPick, onOpenChange };
}

describe("MealPicker", () => {
  it("lists all of my meals, loading every page by itself", async () => {
    const fake = twoPages();
    open();
    const dialog = await screen.findByRole("dialog", { name: "Choose breakfast" });
    expect(await within(dialog).findByRole("button", { name: /Oat bowl/ })).toBeInTheDocument();
    expect(await within(dialog).findByRole("button", { name: /Pasta bowl/ })).toBeInTheDocument();
    expect(fake.callsTo("GET", "/meals")[1]?.search.get("cursor")).toBe("c1");
  });

  it("narrows the loaded meals as you type", async () => {
    twoPages();
    open();
    await screen.findByRole("button", { name: /Pasta bowl/ });
    await userEvent.type(screen.getByLabelText("Search your meals"), "pasta");
    expect(screen.queryByRole("button", { name: /Oat bowl/ })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Pasta bowl/ })).toBeInTheDocument();

    await userEvent.clear(screen.getByLabelText("Search your meals"));
    await userEvent.type(screen.getByLabelText("Search your meals"), "zzz");
    expect(await screen.findByText("No meal matches “zzz”.")).toBeInTheDocument();
  });

  it("hands over the chosen meal and closes", async () => {
    twoPages();
    const { onPick, onOpenChange } = open();
    await userEvent.click(await screen.findByRole("button", { name: /Pasta bowl/ }));
    expect(onPick).toHaveBeenCalledWith(expect.objectContaining({ id: "m2", name: "Pasta bowl" }));
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("points to making a meal when there are none", async () => {
    fakeApi({ "GET /meals": () => json({ items: [], next_cursor: null }) });
    open();
    expect(await screen.findByText("You have no meals yet.")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Create a meal" })).toHaveAttribute("href", "/meals/new");
  });

  it("stops asking for pages after one fails, keeps what it has, and offers a retry", async () => {
    let failing = true;
    const fake = fakeApi({
      "GET /meals": (req) => (req.search.get("cursor") === "c1" ? (failing ? problem(500, "internal_error") : json({ items: [makeSummary({ id: "m2", name: "Pasta bowl" })], next_cursor: null })) : json({ items: [makeSummary()], next_cursor: "c1" })),
    });
    open();
    expect(await screen.findByText("Something went wrong. Try again.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Oat bowl/ })).toBeInTheDocument();
    const asked = fake.callsTo("GET", "/meals").length;
    await new Promise((resolve) => setTimeout(resolve, 150));
    expect(fake.callsTo("GET", "/meals")).toHaveLength(asked);

    failing = false;
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByRole("button", { name: /Pasta bowl/ })).toBeInTheDocument();
    await waitFor(() => expect(screen.queryByText("Something went wrong. Try again.")).not.toBeInTheDocument());
  });
});
