// @vitest-environment jsdom
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fakeApi, json, problem } from "@/test/fake-api";
import { makeSummary, nutrients, unknownNutrients } from "@/test/fixtures";
import { TARGETS, makeDay, makeEntry, makePlan } from "@/test/plan-fixtures";
import { renderWithClient } from "@/test/render";
import { TodayView } from "./today-view";

// 23:30 local: the UTC date is already tomorrow's in a positive offset. The page must ask for the local day.
const DAY = "2026-09-30";
beforeEach(() => vi.useFakeTimers({ toFake: ["Date"], now: new Date(2026, 8, 30, 23, 30) }));
afterEach(() => vi.useRealTimers());

const planFor = (entries = [makeEntry({ date: DAY, meal_name: "Porridge", meal_id: "m1", portion: 1 })], nutrition = nutrients({ calories: 1200, protein: 90, carbohydrates: 150, fat: 40 })) =>
  makePlan(DAY, DAY, [makeDay(DAY, entries, nutrition)], TARGETS);

describe("TodayView", () => {
  it("asks for today's local date and shows it", async () => {
    const fake = fakeApi({ "GET /plan": () => json(planFor()) });
    renderWithClient(<TodayView />);
    expect(await screen.findByText("Wednesday, September 30")).toBeInTheDocument();
    expect(fake.calls[0]?.search.get("from")).toBe(DAY);
    expect(fake.calls[0]?.search.get("to")).toBe(DAY);
  });

  it("shows the day's rings against the targets and the day's meals", async () => {
    fakeApi({ "GET /plan": () => json(planFor()) });
    renderWithClient(<TodayView />);
    const rings = await screen.findByRole("region", { name: "Today's totals" });
    expect(within(rings).getByText("1,200 kcal")).toBeInTheDocument();
    expect(within(rings).getByText("60% of 2,000 kcal")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Porridge" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Add meal for lunch" })).toBeInTheDocument();
  });

  it("swaps breakfast in one tap: the new meal shows at once and the day's totals are refetched", async () => {
    let server = planFor();
    const fake = fakeApi({
      "GET /plan": () => json(server),
      "GET /meals": () => json({ items: [makeSummary({ id: "m2", name: "Pasta bowl" })], next_cursor: null }),
      "PUT /plan/:date/:slot": () => {
        server = planFor([makeEntry({ date: DAY, meal_id: "m2", meal_name: "Pasta bowl" })], nutrients({ calories: 1500 }));
        return json(makeEntry({ date: DAY, meal_id: "m2", meal_name: "Pasta bowl" }));
      },
    });
    renderWithClient(<TodayView />);
    await userEvent.click(await screen.findByRole("button", { name: "Swap breakfast meal" }));
    await userEvent.click(await screen.findByRole("button", { name: /Pasta bowl/ }));

    expect(await screen.findByRole("link", { name: "Pasta bowl" })).toBeInTheDocument();
    expect(fake.callsTo("PUT", `/plan/${DAY}/breakfast`)[0]?.body).toEqual({ meal_id: "m2", portion: 1 });
    expect(await screen.findByText("1,500 kcal")).toBeInTheDocument();
  });

  it("invites you to plan an empty day", async () => {
    fakeApi({ "GET /plan": () => json(planFor([], nutrients())) });
    renderWithClient(<TodayView />);
    expect(await screen.findByText(/Nothing planned for today yet/)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Apply a diet template" })).toHaveAttribute("href", "/plan");
    expect(screen.getByRole("button", { name: "Add meal for breakfast" })).toBeInTheDocument();
  });

  it("shows a nutrient that some ingredient lacks as a dash, not zero", async () => {
    fakeApi({ "GET /plan": () => json(planFor([makeEntry({ date: DAY })], unknownNutrients({ calories: 800 }))) });
    renderWithClient(<TodayView />);
    const rings = await screen.findByRole("region", { name: "Today's totals" });
    expect(within(within(rings).getByRole("group", { name: "Protein" })).getByText("—")).toBeInTheDocument();
  });

  it("offers a retry when the plan cannot be loaded", async () => {
    let fail = true;
    fakeApi({ "GET /plan": () => (fail ? problem(500, "internal_error") : json(planFor())) });
    renderWithClient(<TodayView />);
    expect(await screen.findByText("Something went wrong. Try again.")).toBeInTheDocument();
    fail = false;
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByRole("link", { name: "Porridge" })).toBeInTheDocument();
  });
});
