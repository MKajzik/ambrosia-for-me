// @vitest-environment jsdom
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { fakeApi, json, noContent, problem } from "@/test/fake-api";
import { makeSummary, partnership } from "@/test/fixtures";
import { makeTemplate, makeTemplateSlot } from "@/test/plan-fixtures";
import { renderWithClient } from "@/test/render";
import { TemplateEditor } from "./template-editor";

const replace = vi.fn();
vi.mock("next/navigation", () => ({ useRouter: () => ({ replace: (url: string) => replace(url), push: vi.fn() }) }));
afterEach(() => replace.mockReset());

const startTemplate = () =>
  makeTemplate({ day_count: 2, slots: [makeTemplateSlot({ id: "s1", day_index: 0, slot: "breakfast", meal_id: "m1", meal_name: "Oat bowl", portion: 2 })] });
const catalogue = () => json({ items: [makeSummary({ id: "m2", name: "Pasta bowl" }), makeSummary()], next_cursor: null });

function setup(routes: Parameters<typeof fakeApi>[0] = {}, { delay = 10, template = startTemplate() } = {}) {
  const fake = fakeApi({
    "GET /partner": () => problem(404, "partner_not_linked"),
    "GET /meals": catalogue,
    "PUT /diet-templates/:id/slots": () => json(makeTemplate()),
    ...routes,
  });
  const view = renderWithClient(<TemplateEditor template={template} autosaveDelayMs={delay} />);
  return { fake, ...view };
}
const putSlots = (fake: ReturnType<typeof fakeApi>) => fake.callsTo("PUT", "/diet-templates/t1/slots");
const patchTemplate = (fake: ReturnType<typeof fakeApi>) => fake.callsTo("PATCH", "/diet-templates/t1");
const pickInDialog = async (title: string, meal: RegExp) => {
  const dialog = await screen.findByRole("dialog", { name: title });
  await userEvent.click(await within(dialog).findByRole("button", { name: meal }));
};
const nothingElseHappens = () => new Promise((resolve) => setTimeout(resolve, 120));

describe("TemplateEditor", () => {
  it("shows the template as saved: its name, its fixed length and every day's slots", () => {
    setup();
    expect(screen.getByLabelText("Name", { exact: true })).toHaveValue("Base week");
    expect(screen.getByText("2 days")).toBeInTheDocument();
    const day1 = within(screen.getByRole("region", { name: "Day 1" }));
    expect(day1.getByRole("link", { name: "Oat bowl" })).toHaveAttribute("href", "/meals/m1");
    expect(day1.getByLabelText("Portion of breakfast on day 1")).toHaveValue("2");
    expect(within(screen.getByRole("region", { name: "Day 2" })).getByRole("button", { name: "Add breakfast to day 2" })).toBeInTheDocument();
    expect(screen.getByText("All changes saved")).toBeInTheDocument();
  });

  it("adds a meal to an empty slot and saves the whole slot list in day order", async () => {
    const { fake } = setup();
    await userEvent.click(screen.getByRole("button", { name: "Add lunch to day 2" }));
    await pickInDialog("Choose lunch for day 2", /Pasta bowl/);

    await waitFor(() => expect(putSlots(fake)).toHaveLength(1));
    expect(putSlots(fake)[0]?.body).toEqual({
      items: [
        { day_index: 0, slot: "breakfast", meal_id: "m1", portion: 2 },
        { day_index: 1, slot: "lunch", meal_id: "m2", portion: 1 },
      ],
    });
    expect(within(screen.getByRole("region", { name: "Day 2" })).getByRole("link", { name: "Pasta bowl" })).toBeInTheDocument();
  });

  it("swaps a meal and keeps its portion", async () => {
    const { fake } = setup();
    await userEvent.click(screen.getByRole("button", { name: "Swap breakfast on day 1" }));
    await pickInDialog("Choose breakfast for day 1", /Pasta bowl/);
    await waitFor(() => expect(putSlots(fake)).toHaveLength(1));
    expect(putSlots(fake)[0]?.body).toEqual({ items: [{ day_index: 0, slot: "breakfast", meal_id: "m2", portion: 2 }] });
  });

  it("removes a meal", async () => {
    const { fake } = setup();
    await userEvent.click(screen.getByRole("button", { name: "Remove breakfast from day 1" }));
    await waitFor(() => expect(putSlots(fake)).toHaveLength(1));
    expect(putSlots(fake)[0]?.body).toEqual({ items: [] });
  });

  it("takes a portion typed with a comma", async () => {
    const { fake } = setup();
    fireEvent.change(screen.getByLabelText("Portion of breakfast on day 1"), { target: { value: "1,5" } });
    await waitFor(() => expect(putSlots(fake)).toHaveLength(1));
    expect(putSlots(fake)[0]?.body).toEqual({ items: [{ day_index: 0, slot: "breakfast", meal_id: "m1", portion: 1.5 }] });
  });

  it("flags a portion it cannot use, sends nothing, and keeps what was typed", async () => {
    const { fake } = setup();
    fireEvent.change(screen.getByLabelText("Portion of breakfast on day 1"), { target: { value: "0" } });
    expect(await screen.findByText("The portion must be more than 0 and at most 100.")).toBeInTheDocument();
    expect(screen.getByText("Fix the highlighted fields to save.")).toBeInTheDocument();
    await nothingElseHappens();
    expect(putSlots(fake)).toHaveLength(0);
    expect(screen.getByLabelText("Portion of breakfast on day 1")).toHaveValue("0");
  });

  it("lets a day hold several snacks, each with its own portion", async () => {
    const { fake } = setup();
    await userEvent.click(screen.getByRole("button", { name: "Add snack to day 1" }));
    await pickInDialog("Add a snack to day 1", /Pasta bowl/);
    await waitFor(() => expect(putSlots(fake)).toHaveLength(1));
    await userEvent.click(screen.getByRole("button", { name: "Add snack to day 1" }));
    await pickInDialog("Add a snack to day 1", /Oat bowl/);
    await waitFor(() => expect(putSlots(fake)).toHaveLength(2));
    expect(putSlots(fake)[1]?.body).toEqual({
      items: [
        { day_index: 0, slot: "breakfast", meal_id: "m1", portion: 2 },
        { day_index: 0, slot: "snack", meal_id: "m2", portion: 1 },
        { day_index: 0, slot: "snack", meal_id: "m1", portion: 1 },
      ],
    });
    expect(screen.getByLabelText("Portion of snack on day 1 (Pasta bowl)")).toBeInTheDocument();
    expect(screen.getByLabelText("Portion of snack on day 1 (Oat bowl)")).toBeInTheDocument();
  });

  it("saves a rename, sending only the name", async () => {
    const { fake } = setup({ "PATCH /diet-templates/:id": () => json(makeTemplate({ name: "Cut" })) });
    fireEvent.change(screen.getByLabelText("Name", { exact: true }), { target: { value: "Cut" } });
    await waitFor(() => expect(patchTemplate(fake)).toHaveLength(1));
    expect(patchTemplate(fake)[0]?.body).toEqual({ name: "Cut" });
    expect(putSlots(fake)).toHaveLength(0);
    expect(await screen.findByText("All changes saved")).toBeInTheDocument();
  });

  it("explains a refusal from the server, keeps the draft, and does not retry by itself", async () => {
    const { fake } = setup({ "PUT /diet-templates/:id/slots": () => problem(409, "duplicate_slot") });
    fireEvent.change(screen.getByLabelText("Portion of breakfast on day 1"), { target: { value: "3" } });
    expect(await screen.findByText("That day already has a meal for that slot.")).toBeInTheDocument();
    expect(screen.getByLabelText("Portion of breakfast on day 1")).toHaveValue("3");
    await nothingElseHappens();
    expect(putSlots(fake)).toHaveLength(1);
  });

  it("offers a retry after a failed save and saves the same draft again", async () => {
    let fail = true;
    const { fake } = setup({ "PUT /diet-templates/:id/slots": () => (fail ? problem(500, "internal_error") : json(makeTemplate())) });
    fireEvent.change(screen.getByLabelText("Portion of breakfast on day 1"), { target: { value: "3" } });
    expect(await screen.findByText("Something went wrong. Try again.")).toBeInTheDocument();
    fail = false;
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByText("All changes saved")).toBeInTheDocument();
    expect(putSlots(fake)).toHaveLength(2);
  });

  it("saves a pending edit when the editor is closed before the pause is over", async () => {
    const { fake, unmount } = setup({}, { delay: 60_000 });
    fireEvent.change(screen.getByLabelText("Portion of breakfast on day 1"), { target: { value: "3" } });
    expect(putSlots(fake)).toHaveLength(0);
    unmount();
    await waitFor(() => expect(putSlots(fake)).toHaveLength(1));
  });

  it("offers sharing only while linked with a partner, and saves the choice", async () => {
    const { fake } = setup({ "GET /partner": () => json(partnership()), "PATCH /diet-templates/:id": () => json(makeTemplate({ shared_with_partner: true })) });
    await userEvent.click(await screen.findByLabelText("Share with my partner"));
    await waitFor(() => expect(patchTemplate(fake)).toHaveLength(1));
    expect(patchTemplate(fake)[0]?.body).toEqual({ shared_with_partner: true });
  });

  it("hides the sharing choice when there is no partner", async () => {
    const { queryClient } = setup();
    await waitFor(() => expect(queryClient.getQueryState(["partner"])?.status).toBe("success"));
    expect(screen.queryByLabelText("Share with my partner")).not.toBeInTheDocument();
  });

  it("deletes the template after a confirmation, saying that planned meals stay, and returns to the library", async () => {
    const { fake } = setup({ "DELETE /diet-templates/:id": () => noContent() });
    await userEvent.click(screen.getByRole("button", { name: "Delete template" }));
    const dialog = await screen.findByRole("dialog", { name: "Delete this template?" });
    expect(within(dialog).getByText(/Meals already in your plan stay where they are/)).toBeInTheDocument();
    await userEvent.click(within(dialog).getByRole("button", { name: "Delete template" }));
    await waitFor(() => expect(replace).toHaveBeenCalledWith("/plan/templates"));
    expect(fake.callsTo("DELETE", "/diet-templates/t1")).toHaveLength(1);
  });

  it("stays put and says why when the delete fails", async () => {
    setup({ "DELETE /diet-templates/:id": () => problem(500, "internal_error") });
    await userEvent.click(screen.getByRole("button", { name: "Delete template" }));
    const dialog = await screen.findByRole("dialog", { name: "Delete this template?" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Delete template" }));
    expect(await within(dialog).findByText("Something went wrong. Try again.")).toBeInTheDocument();
    expect(replace).not.toHaveBeenCalled();
  });
});
