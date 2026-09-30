// @vitest-environment jsdom
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { PARTNER_KEY } from "@/features/meals/queries";
import { fakeApi, json, problem } from "@/test/fake-api";
import { partnership } from "@/test/fixtures";
import { renderWithClient } from "@/test/render";
import { makeList, makeListSummary } from "@/test/shopping-fixtures";
import { ShoppingPage } from "./shopping-page";

const push = vi.fn();
vi.mock("next/navigation", () => ({ useRouter: () => ({ push: (url: string) => push(url), replace: vi.fn() }) }));

// Wednesday 30 September 2026: the week is Monday 28 September to Sunday 4 October.
beforeEach(() => vi.useFakeTimers({ toFake: ["Date"], now: new Date(2026, 8, 30, 10, 0) }));
afterEach(() => {
  vi.useRealTimers();
  push.mockReset();
});

const page = (items: ReturnType<typeof makeListSummary>[], next_cursor: string | null = null) => json({ items, next_cursor });
const noPartner = { "GET /partner": () => problem(404, "partner_not_linked") };

async function partnerSettled(queryClient: ReturnType<typeof renderWithClient>["queryClient"]) {
  await waitFor(() => expect(queryClient.getQueryState(PARTNER_KEY)?.status).toBe("success"));
}

describe("ShoppingPage lists", () => {
  it("lists my lists with the plan range they were built from and a shared badge, and no tabs without a partner", async () => {
    fakeApi({
      ...noPartner,
      "GET /shopping-lists": () =>
        page([makeListSummary({ shared_with_partner: true, source_from: "2026-09-28", source_to: "2026-10-04" }), makeListSummary({ id: "l2", name: "Party" })]),
    });
    const { queryClient } = renderWithClient(<ShoppingPage />);
    const weekly = await screen.findByRole("link", { name: /Weekly shop/ });
    expect(weekly).toHaveAttribute("href", "/shopping/l1");
    expect(weekly).toHaveTextContent("Sep 28 – Oct 4");
    expect(screen.getByRole("link", { name: /Party/ })).toHaveAttribute("href", "/shopping/l2");
    expect(screen.getByText("Shared")).toBeInTheDocument();
    await partnerSettled(queryClient);
    expect(screen.queryByRole("tab")).not.toBeInTheDocument();
  });

  it("asks for the next page with the API's cursor", async () => {
    const fake = fakeApi({
      ...noPartner,
      "GET /shopping-lists": (req) => (req.search.get("cursor") === "c1" ? page([makeListSummary({ id: "l2", name: "Party" })]) : page([makeListSummary()], "c1")),
    });
    renderWithClient(<ShoppingPage />);
    await userEvent.click(await screen.findByRole("button", { name: "Load more" }));
    expect(await screen.findByRole("link", { name: /Party/ })).toBeInTheDocument();
    expect(fake.callsTo("GET", "/shopping-lists")[1]?.search.get("cursor")).toBe("c1");
  });

  it("invites you to start a list when there are none, with the same actions as the header", async () => {
    fakeApi({ ...noPartner, "GET /shopping-lists": () => page([]) });
    renderWithClient(<ShoppingPage />);
    expect(await screen.findByText("No shopping lists yet")).toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: "Generate from plan" })).toHaveLength(2);
  });

  it("offers a retry when the lists cannot load", async () => {
    let fail = true;
    fakeApi({ ...noPartner, "GET /shopping-lists": () => (fail ? problem(500, "internal_error") : page([makeListSummary()])) });
    renderWithClient(<ShoppingPage />);
    expect(await screen.findByText("Something went wrong. Try again.")).toBeInTheDocument();
    fail = false;
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByRole("link", { name: /Weekly shop/ })).toBeInTheDocument();
  });

  it("shows the partner's shared lists under their own tab", async () => {
    fakeApi({
      "GET /partner": () => json(partnership()),
      "GET /shopping-lists": () => page([makeListSummary()]),
      "GET /partner/shopping-lists": () => page([makeListSummary({ id: "p1", name: "Barbecue", shared_with_partner: true })]),
    });
    renderWithClient(<ShoppingPage />);
    await userEvent.click(await screen.findByRole("tab", { name: "Partner's" }));
    const link = await screen.findByRole("link", { name: /Barbecue/ });
    expect(link).toHaveAttribute("href", "/shopping/p1");
    expect(screen.queryByRole("link", { name: /Weekly shop/ })).not.toBeInTheDocument();
  });

  it("drops the partner tab, and shows my lists again, when the partner unlinks while it is open", async () => {
    let linked = true;
    fakeApi({
      "GET /partner": () => (linked ? json(partnership()) : problem(404, "partner_not_linked")),
      "GET /shopping-lists": () => page([makeListSummary()]),
      "GET /partner/shopping-lists": () => problem(404, "partner_not_linked"),
    });
    renderWithClient(<ShoppingPage />);
    const tab = await screen.findByRole("tab", { name: "Partner's" });
    linked = false;
    await userEvent.click(tab);
    await waitFor(() => expect(screen.queryByRole("tab")).not.toBeInTheDocument());
    expect(await screen.findByRole("link", { name: /Weekly shop/ })).toBeInTheDocument();
  });
});

describe("New list", () => {
  it("creates an empty list and opens it", async () => {
    const fake = fakeApi({ ...noPartner, "GET /shopping-lists": () => page([makeListSummary()]), "POST /shopping-lists": () => json(makeList({ id: "new1", name: "Party" }), 201) });
    renderWithClient(<ShoppingPage />);
    await userEvent.click(await screen.findByRole("button", { name: "New list" }));
    const dialog = await screen.findByRole("dialog", { name: "New shopping list" });
    await userEvent.type(within(dialog).getByLabelText("Name"), "Party");
    await userEvent.click(within(dialog).getByRole("button", { name: "Create list" }));

    await waitFor(() => expect(push).toHaveBeenCalledWith("/shopping/new1"));
    expect(fake.callsTo("POST", "/shopping-lists")[0]?.body).toEqual({ name: "Party" });
  });

  it("asks for a name and sends nothing without one", async () => {
    const fake = fakeApi({ ...noPartner, "GET /shopping-lists": () => page([makeListSummary()]) });
    renderWithClient(<ShoppingPage />);
    await userEvent.click(await screen.findByRole("button", { name: "New list" }));
    const dialog = await screen.findByRole("dialog", { name: "New shopping list" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Create list" }));
    expect(await within(dialog).findByText("Give the list a name.")).toBeInTheDocument();
    expect(fake.callsTo("POST", "/shopping-lists")).toHaveLength(0);
  });
});

describe("Generate from plan", () => {
  async function openGenerate() {
    await userEvent.click((await screen.findAllByRole("button", { name: "Generate from plan" }))[0]!);
    return screen.findByRole("dialog", { name: "Generate from your plan" });
  }

  it("defaults to this week, generates, and opens the new list", async () => {
    const fake = fakeApi({ ...noPartner, "GET /shopping-lists": () => page([makeListSummary()]), "POST /shopping-lists/generate": () => json(makeList({ id: "gen1" }), 201) });
    renderWithClient(<ShoppingPage />);
    const dialog = await openGenerate();
    expect(within(dialog).getByLabelText("From")).toHaveValue("2026-09-28");
    expect(within(dialog).getByLabelText("To")).toHaveValue("2026-10-04");
    await userEvent.click(within(dialog).getByRole("button", { name: "Generate list" }));

    await waitFor(() => expect(push).toHaveBeenCalledWith("/shopping/gen1"));
    expect(fake.callsTo("POST", "/shopping-lists/generate")[0]?.body).toEqual({ from: "2026-09-28", to: "2026-10-04" });
  });

  it("sends the range and the name the person chose", async () => {
    const fake = fakeApi({ ...noPartner, "GET /shopping-lists": () => page([makeListSummary()]), "POST /shopping-lists/generate": () => json(makeList({ id: "gen1" }), 201) });
    renderWithClient(<ShoppingPage />);
    const dialog = await openGenerate();
    fireEvent.change(within(dialog).getByLabelText("From"), { target: { value: "2026-10-05" } });
    fireEvent.change(within(dialog).getByLabelText("To"), { target: { value: "2026-10-07" } });
    await userEvent.type(within(dialog).getByLabelText("List name (optional)"), "Midweek");
    await userEvent.click(within(dialog).getByRole("button", { name: "Generate list" }));
    await waitFor(() => expect(fake.callsTo("POST", "/shopping-lists/generate")).toHaveLength(1));
    expect(fake.callsTo("POST", "/shopping-lists/generate")[0]?.body).toEqual({ from: "2026-10-05", to: "2026-10-07", name: "Midweek" });
  });

  it.each([
    ["an end before the start", "2026-10-07", "2026-10-05", "The end date must be on or after the start date."],
    ["more than 92 days", "2026-01-01", "2026-04-03", "Pick at most 92 days."],
    ["a blank date", "", "2026-10-05", "Choose a start and end date."],
  ])("refuses %s inline and sends nothing", async (_name, from, to, message) => {
    const fake = fakeApi({ ...noPartner, "GET /shopping-lists": () => page([makeListSummary()]) });
    renderWithClient(<ShoppingPage />);
    const dialog = await openGenerate();
    fireEvent.change(within(dialog).getByLabelText("From"), { target: { value: from } });
    fireEvent.change(within(dialog).getByLabelText("To"), { target: { value: to } });
    await userEvent.click(within(dialog).getByRole("button", { name: "Generate list" }));
    expect(await within(dialog).findByText(message)).toBeInTheDocument();
    expect(fake.callsTo("POST", "/shopping-lists/generate")).toHaveLength(0);
  });

  it("keeps the dialog open when the server refuses", async () => {
    fakeApi({ ...noPartner, "GET /shopping-lists": () => page([makeListSummary()]), "POST /shopping-lists/generate": () => problem(400, "plan_range_too_long") });
    renderWithClient(<ShoppingPage />);
    const dialog = await openGenerate();
    await userEvent.click(within(dialog).getByRole("button", { name: "Generate list" }));
    await waitFor(() => expect(within(dialog).getByRole("button", { name: "Generate list" })).toBeEnabled());
    expect(push).not.toHaveBeenCalled();
    expect(screen.getByRole("dialog", { name: "Generate from your plan" })).toBeInTheDocument();
  });
});
