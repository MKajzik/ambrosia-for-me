// @vitest-environment jsdom
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { fakeApi, json, noContent, problem } from "@/test/fake-api";
import { makeSummary } from "@/test/fixtures";
import { makeDay, makeEntry, makePlan } from "@/test/plan-fixtures";
import { renderWithClient } from "@/test/render";
import { DayMeals } from "./day-meals";
import { usePlan } from "./queries";

const toastError = vi.fn();
vi.mock("sonner", () => ({ toast: { error: (message: string) => toastError(message), success: vi.fn() } }));
afterEach(() => toastError.mockReset());

const D = "2026-09-28";

function Harness() {
  const plan = usePlan(D, D);
  const day = plan.data?.days[0];
  return day ? <DayMeals date={D} entries={day.entries} /> : null;
}

const porridgeDay = () => makePlan(D, D, [makeDay(D, [makeEntry({ meal_id: "m1", meal_name: "Porridge", portion: 1 })])]);
const catalogue = { "GET /meals": () => json({ items: [makeSummary({ id: "m2", name: "Pasta bowl" })], next_cursor: null }) };

describe("DayMeals", () => {
  it("shows all four slots of the day", async () => {
    fakeApi({ "GET /plan": () => json(porridgeDay()) });
    renderWithClient(<Harness />);
    expect(await screen.findByRole("link", { name: "Porridge" })).toBeInTheDocument();
    for (const name of ["Add meal for lunch", "Add meal for dinner", "Add snack"]) expect(screen.getByRole("button", { name })).toBeInTheDocument();
  });

  it("swaps a meal: the new name shows at once and the API gets the date, slot, meal and portion", async () => {
    let release!: () => void;
    const fake = fakeApi({
      "GET /plan": () => json(porridgeDay()),
      ...catalogue,
      "PUT /plan/:date/:slot": () =>
        new Promise<Response>((resolve) => {
          release = () => resolve(json(makeEntry({ meal_id: "m2", meal_name: "Pasta bowl" })));
        }),
    });
    renderWithClient(<Harness />);
    await userEvent.click(await screen.findByRole("button", { name: "Swap breakfast meal" }));
    await userEvent.click(await screen.findByRole("button", { name: /Pasta bowl/ }));

    expect(await screen.findByRole("link", { name: "Pasta bowl" })).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Porridge" })).not.toBeInTheDocument();
    expect(fake.callsTo("PUT", `/plan/${D}/breakfast`)[0]?.body).toEqual({ meal_id: "m2", portion: 1 });
    release();
  });

  it("changes a portion with one tap", async () => {
    let server = porridgeDay();
    const fake = fakeApi({
      "GET /plan": () => json(server),
      "PUT /plan/:date/:slot": () => {
        server = makePlan(D, D, [makeDay(D, [makeEntry({ meal_id: "m1", meal_name: "Porridge", portion: 1.5 })])]);
        return json(makeEntry({ portion: 1.5 }));
      },
    });
    renderWithClient(<Harness />);
    await userEvent.click(await screen.findByRole("button", { name: "Increase portion of Porridge" }));
    expect(await screen.findByText("1.5 servings")).toBeInTheDocument();
    expect(fake.callsTo("PUT", `/plan/${D}/breakfast`)[0]?.body).toEqual({ meal_id: "m1", portion: 1.5 });
  });

  it("puts the old meal back and toasts the reason when the server refuses the swap", async () => {
    fakeApi({ "GET /plan": () => json(porridgeDay()), ...catalogue, "PUT /plan/:date/:slot": () => problem(400, "invalid_meal") });
    renderWithClient(<Harness />);
    await userEvent.click(await screen.findByRole("button", { name: "Swap breakfast meal" }));
    await userEvent.click(await screen.findByRole("button", { name: /Pasta bowl/ }));

    await waitFor(() => expect(toastError).toHaveBeenCalledWith("That meal isn't available. It may have been deleted."));
    expect(await screen.findByRole("link", { name: "Porridge" })).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Pasta bowl" })).not.toBeInTheDocument();
  });

  it("removes a meal", async () => {
    let server = porridgeDay();
    const fake = fakeApi({
      "GET /plan": () => json(server),
      "DELETE /plan/:date/:slot": () => {
        server = makePlan(D, D, [makeDay(D, [])]);
        return noContent();
      },
    });
    renderWithClient(<Harness />);
    await userEvent.click(await screen.findByRole("button", { name: "Remove Porridge from breakfast" }));
    await waitFor(() => expect(screen.queryByRole("link", { name: "Porridge" })).not.toBeInTheDocument());
    expect(fake.callsTo("DELETE", `/plan/${D}/breakfast`)).toHaveLength(1);
  });
});
