// @vitest-environment jsdom
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { fakeApi, json } from "@/test/fake-api";
import { makeTemplate, makeTemplateSlot } from "@/test/plan-fixtures";
import { renderWithClient } from "@/test/render";
import { TemplateView } from "./template-view";

const push = vi.fn();
vi.mock("next/navigation", () => ({ useRouter: () => ({ push: (url: string) => push(url), replace: vi.fn() }) }));
afterEach(() => push.mockReset());

const shared = makeTemplate({
  id: "p1",
  name: "Bulk",
  day_count: 2,
  is_owner: false,
  slots: [
    makeTemplateSlot({ id: "a", day_index: 1, slot: "dinner", meal_name: "Stew", portion: 2 }),
    makeTemplateSlot({ id: "b", day_index: 0, slot: "lunch", meal_name: "Wrap", portion: 1 }),
    makeTemplateSlot({ id: "c", day_index: 0, slot: "breakfast", meal_name: "Oat bowl", portion: 1.5 }),
    makeTemplateSlot({ id: "d", day_index: 0, slot: "snack", meal_name: "Apple", portion: 1 }),
  ],
});

describe("TemplateView", () => {
  it("shows every day's slots in the order of a day, read-only", () => {
    renderWithClient(<TemplateView template={shared} />);
    const day1 = within(screen.getByRole("region", { name: "Day 1" }));
    const rows = day1.getAllByRole("listitem").map((li) => li.textContent);
    expect(rows).toEqual(["BreakfastOat bowl1.5 servings", "LunchWrap1 serving", "SnacksApple1 serving"]);
    const day2 = within(screen.getByRole("region", { name: "Day 2" }));
    expect(day2.getByText("Stew")).toBeInTheDocument();
    expect(day2.getByText("2 servings")).toBeInTheDocument();
    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /apply/i })).not.toBeInTheDocument();
  });

  it("says a day with nothing planned is empty", () => {
    renderWithClient(<TemplateView template={makeTemplate({ is_owner: false, day_count: 2, slots: [makeTemplateSlot()] })} />);
    expect(within(screen.getByRole("region", { name: "Day 2" })).getByText("Nothing planned.")).toBeInTheDocument();
  });

  it("explains that it must be copied to change or apply it, and copies it", async () => {
    const fake = fakeApi({ "POST /diet-templates/:id/copy": () => json(makeTemplate({ id: "copy1", name: "Bulk" }), 201) });
    renderWithClient(<TemplateView template={shared} />);
    expect(screen.getByText("Shared by your partner. Copy it to your library to change or apply it.")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: /Copy to my library/ }));
    await waitFor(() => expect(push).toHaveBeenCalledWith("/plan/templates/copy1"));
    expect(fake.callsTo("POST", "/diet-templates/p1/copy")).toHaveLength(1);
  });
});
