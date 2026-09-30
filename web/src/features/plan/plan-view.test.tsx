// @vitest-environment jsdom
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { addDays, weekDates } from "@/lib/dates";
import { fakeApi, json, noContent, problem } from "@/test/fake-api";
import { nutrients, unknownNutrients } from "@/test/fixtures";
import { makeDay, makeEntry, makePlan, makeTemplateSummary } from "@/test/plan-fixtures";
import { renderWithClient } from "@/test/render";
import { PlanView } from "./plan-view";

// Wednesday 30 September 2026: the week is Monday 28 September to Sunday 4 October.
beforeEach(() => vi.useFakeTimers({ toFake: ["Date"], now: new Date(2026, 8, 30, 10, 0) }));
afterEach(() => vi.useRealTimers());

/** A week from `from`; `kcal` maps a date to that day's calories (a day with calories gets one meal). */
function week(from: string, kcal: Record<string, number | null> = {}) {
  return makePlan(
    from,
    addDays(from, 6),
    weekDates(from).map((date) => {
      const value = kcal[date];
      const entries = value === undefined ? [] : [makeEntry({ date, meal_name: `Meal ${date}` })];
      const nutrition = value === null ? unknownNutrients() : nutrients({ calories: value ?? 0, protein: (value ?? 0) / 10 });
      return makeDay(date, entries, nutrition);
    }),
  );
}

const planRoute = (kcal: Record<string, number | null> = {}) => ({ "GET /plan": (req: { search: URLSearchParams }) => json(week(req.search.get("from") ?? "2026-09-28", kcal)) });
const templates = { "GET /diet-templates": () => json({ items: [makeTemplateSummary({ id: "t1", name: "Base week", day_count: 7 })], next_cursor: null }) };

describe("PlanView", () => {
  it("shows the Monday-to-Sunday week that holds today, with today marked", async () => {
    const fake = fakeApi(planRoute());
    renderWithClient(<PlanView />);
    expect(await screen.findByText("Sep 28 – Oct 4")).toBeInTheDocument();
    expect(fake.calls[0]?.search.get("from")).toBe("2026-09-28");
    expect(fake.calls[0]?.search.get("to")).toBe("2026-10-04");
    const wednesday = await screen.findByRole("region", { name: "Wednesday, September 30" });
    expect(within(wednesday).getByText("Today")).toBeInTheDocument();
    expect(screen.getAllByText("Today")).toHaveLength(1);
    expect(screen.getByRole("region", { name: "Monday, September 28" })).toBeInTheDocument();
    expect(screen.getByRole("region", { name: "Sunday, October 4" })).toBeInTheDocument();
  });

  it("shows each day's calories against the target, and the week's total and daily average", async () => {
    fakeApi(planRoute({ "2026-09-28": 600, "2026-09-30": 400 }));
    renderWithClient(<PlanView />);
    const monday = await screen.findByRole("region", { name: "Monday, September 28" });
    expect(within(monday).getByText(/600 kcal of 2,000 kcal/)).toBeInTheDocument();

    const totals = screen.getByRole("region", { name: "Week totals" });
    expect(within(totals).getByText("1,000 kcal")).toBeInTheDocument();
    expect(within(totals).getByText("143 kcal")).toBeInTheDocument();
    expect(within(totals).getByText("7% of your 2,000 kcal target")).toBeInTheDocument();
  });

  it("does not turn an unknown day into zero: the week total is unknown too", async () => {
    fakeApi(planRoute({ "2026-09-28": 600, "2026-09-29": null }));
    renderWithClient(<PlanView />);
    const tuesday = await screen.findByRole("region", { name: "Tuesday, September 29" });
    expect(within(tuesday).getByText(/^—/)).toBeInTheDocument();
    const totals = screen.getByRole("region", { name: "Week totals" });
    expect(within(totals).getAllByText("—").length).toBeGreaterThanOrEqual(2);
    expect(within(totals).queryByText("600 kcal")).not.toBeInTheDocument();
  });

  it("moves between weeks and back to this one", async () => {
    const fake = fakeApi(planRoute());
    renderWithClient(<PlanView />);
    await screen.findByText("Sep 28 – Oct 4");
    expect(screen.getByRole("button", { name: "This week" })).toBeDisabled();

    await userEvent.click(screen.getByRole("button", { name: "Next week" }));
    expect(await screen.findByText("Oct 5 – Oct 11")).toBeInTheDocument();
    expect(fake.calls.at(-1)?.search.get("from")).toBe("2026-10-05");
    expect(screen.getByRole("button", { name: "This week" })).toBeEnabled();

    await userEvent.click(screen.getByRole("button", { name: "Previous week" }));
    await userEvent.click(screen.getByRole("button", { name: "Previous week" }));
    expect(await screen.findByText("Sep 21 – Sep 27")).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "This week" }));
    expect(await screen.findByText("Sep 28 – Oct 4")).toBeInTheDocument();
  });

  it("links to the diet templates", async () => {
    fakeApi(planRoute());
    renderWithClient(<PlanView />);
    expect(await screen.findByRole("link", { name: "Diet templates" })).toHaveAttribute("href", "/plan/templates");
  });

  it("offers a retry when the week cannot be loaded", async () => {
    let fail = true;
    fakeApi({ "GET /plan": () => (fail ? problem(500, "internal_error") : json(week("2026-09-28"))) });
    renderWithClient(<PlanView />);
    expect(await screen.findByText("Something went wrong. Try again.")).toBeInTheDocument();
    fail = false;
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByText("Sep 28 – Oct 4")).toBeInTheDocument();
  });
});

describe("applying a template", () => {
  async function openDialog() {
    await screen.findByText("Sep 28 – Oct 4");
    await userEvent.click(screen.getByRole("button", { name: "Apply template" }));
    return screen.findByRole("dialog", { name: "Apply a diet template" });
  }

  it("applies my template from the chosen date, then shows the week that date is in", async () => {
    const fake = fakeApi({ ...planRoute(), ...templates, "POST /diet-templates/:id/apply": () => noContent() });
    renderWithClient(<PlanView />);
    const dialog = await openDialog();
    expect(await within(dialog).findByRole("option", { name: "Base week (7 days)" })).toBeInTheDocument();
    expect(within(dialog).getByLabelText("Start date")).toHaveValue("2026-09-28");

    fireEvent.change(within(dialog).getByLabelText("Start date"), { target: { value: "2026-10-05" } });
    await userEvent.click(within(dialog).getByRole("button", { name: "Apply template" }));

    await waitFor(() => expect(fake.callsTo("POST", "/diet-templates/t1/apply")).toHaveLength(1));
    expect(fake.callsTo("POST", "/diet-templates/t1/apply")[0]?.body).toEqual({ start_date: "2026-10-05", overwrite: false });
    expect(await screen.findByText("Oct 5 – Oct 11")).toBeInTheDocument();
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  });

  it("asks before replacing, and leaves the plan alone when the person keeps it", async () => {
    const fake = fakeApi({ ...planRoute(), ...templates, "POST /diet-templates/:id/apply": () => problem(409, "plan_conflict") });
    renderWithClient(<PlanView />);
    const dialog = await openDialog();
    await within(dialog).findByRole("option", { name: "Base week (7 days)" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Apply template" }));

    expect(await within(dialog).findByText(/Some of those days already have meals\. Replace them\?/)).toBeInTheDocument();
    await userEvent.click(within(dialog).getByRole("button", { name: "Keep my plan" }));
    expect(within(dialog).queryByText(/Replace them\?/)).not.toBeInTheDocument();
    expect(fake.callsTo("POST", "/diet-templates/t1/apply")).toHaveLength(1);
    expect(screen.getByRole("dialog", { name: "Apply a diet template" })).toBeInTheDocument();
  });

  it("retries once with overwrite when the person says replace", async () => {
    let attempts = 0;
    const fake = fakeApi({
      ...planRoute(),
      ...templates,
      "POST /diet-templates/:id/apply": () => (++attempts === 1 ? problem(409, "plan_conflict") : noContent()),
    });
    renderWithClient(<PlanView />);
    const dialog = await openDialog();
    await within(dialog).findByRole("option", { name: "Base week (7 days)" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Apply template" }));
    await userEvent.click(await within(dialog).findByRole("button", { name: "Replace them" }));

    await waitFor(() => expect(fake.callsTo("POST", "/diet-templates/t1/apply")).toHaveLength(2));
    expect(fake.callsTo("POST", "/diet-templates/t1/apply").map((c) => c.body)).toEqual([
      { start_date: "2026-09-28", overwrite: false },
      { start_date: "2026-09-28", overwrite: true },
    ]);
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  });

  it("refuses a start date that is not a date, and sends nothing", async () => {
    const fake = fakeApi({ ...planRoute(), ...templates });
    renderWithClient(<PlanView />);
    const dialog = await openDialog();
    await within(dialog).findByRole("option", { name: "Base week (7 days)" });
    fireEvent.change(within(dialog).getByLabelText("Start date"), { target: { value: "" } });
    await userEvent.click(within(dialog).getByRole("button", { name: "Apply template" }));
    expect(await within(dialog).findByText("Pick a start date.")).toBeInTheDocument();
    expect(fake.callsTo("POST", "/diet-templates/t1/apply")).toHaveLength(0);
  });

  it("says so, and points to making one, when there are no templates", async () => {
    fakeApi({ ...planRoute(), "GET /diet-templates": () => json({ items: [], next_cursor: null }) });
    renderWithClient(<PlanView />);
    const dialog = await openDialog();
    expect(await within(dialog).findByText("You have no diet templates yet.")).toBeInTheDocument();
    expect(within(dialog).getByRole("link", { name: "Create a template" })).toHaveAttribute("href", "/plan/templates/new");
    expect(within(dialog).queryByRole("button", { name: "Apply template" })).not.toBeInTheDocument();
  });

  it("explains any other failure in a toast-worthy message and keeps the dialog open", async () => {
    fakeApi({ ...planRoute(), ...templates, "POST /diet-templates/:id/apply": () => problem(404, "not_found") });
    renderWithClient(<PlanView />);
    const dialog = await openDialog();
    await within(dialog).findByRole("option", { name: "Base week (7 days)" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Apply template" }));
    await waitFor(() => expect(within(dialog).getByRole("button", { name: "Apply template" })).toBeEnabled());
    expect(screen.getByRole("dialog", { name: "Apply a diet template" })).toBeInTheDocument();
    expect(within(dialog).queryByText(/Replace them\?/)).not.toBeInTheDocument();
  });
});
