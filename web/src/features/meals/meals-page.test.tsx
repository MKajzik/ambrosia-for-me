// @vitest-environment jsdom
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { fakeApi, json, problem } from "@/test/fake-api";
import { makeMeal, makeSummary, partnership } from "@/test/fixtures";
import { renderWithClient } from "@/test/render";
import { MealsPage } from "./meals-page";
import { PARTNER_KEY } from "./queries";

const push = vi.fn();
vi.mock("next/navigation", () => ({ useRouter: () => ({ push: (url: string) => push(url), replace: vi.fn() }) }));

afterEach(() => push.mockReset());

const page = (items: ReturnType<typeof makeSummary>[], next_cursor: string | null = null) => json({ items, next_cursor });

async function partnerSettled(queryClient: ReturnType<typeof renderWithClient>["queryClient"]) {
  await waitFor(() => expect(queryClient.getQueryState(PARTNER_KEY)?.status).toBe("success"));
}

describe("MealsPage", () => {
  it("lists my meals with a way to make a new one, and shows no tabs without a partner", async () => {
    fakeApi({
      "GET /partner": () => problem(404, "partner_not_linked"),
      "GET /meals": () => page([makeSummary({ shared_with_partner: true }), makeSummary({ id: "m2", name: "Pasta", servings: 1, notes: "Weeknight" })]),
    });
    const { queryClient } = renderWithClient(<MealsPage />);

    expect(await screen.findByRole("link", { name: /Oat bowl/ })).toHaveAttribute("href", "/meals/m1");
    expect(screen.getByRole("link", { name: /Oat bowl/ })).toHaveTextContent("2 servings");
    expect(screen.getByRole("link", { name: /Pasta/ })).toHaveTextContent("1 serving · Weeknight");
    expect(screen.getByText("Shared")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /New meal/ })).toHaveAttribute("href", "/meals/new");

    await partnerSettled(queryClient);
    expect(screen.queryByRole("tab")).not.toBeInTheDocument();
  });

  it("asks for the next page with the API's cursor", async () => {
    const fake = fakeApi({
      "GET /partner": () => problem(404, "partner_not_linked"),
      "GET /meals": (req) => (req.search.get("cursor") === "c1" ? page([makeSummary({ id: "m2", name: "Pasta" })]) : page([makeSummary()], "c1")),
    });
    renderWithClient(<MealsPage />);
    await userEvent.click(await screen.findByRole("button", { name: "Load more" }));
    expect(await screen.findByRole("link", { name: /Pasta/ })).toBeInTheDocument();
    expect(fake.callsTo("GET", "/meals")[1]?.search.get("cursor")).toBe("c1");
    expect(screen.queryByRole("button", { name: "Load more" })).not.toBeInTheDocument();
  });

  it("invites you to make your first meal when there are none", async () => {
    fakeApi({ "GET /partner": () => problem(404, "partner_not_linked"), "GET /meals": () => page([]) });
    renderWithClient(<MealsPage />);
    expect(await screen.findByText("No meals yet")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Create your first meal" })).toHaveAttribute("href", "/meals/new");
  });

  it("offers a retry when the list cannot load", async () => {
    let fail = true;
    fakeApi({
      "GET /partner": () => problem(404, "partner_not_linked"),
      "GET /meals": () => (fail ? problem(500, "internal_error") : page([makeSummary()])),
    });
    renderWithClient(<MealsPage />);
    const alert = await screen.findByText("Something went wrong. Try again.");
    expect(alert).toBeInTheDocument();
    fail = false;
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByRole("link", { name: /Oat bowl/ })).toBeInTheDocument();
  });

  it("shows the partner's shared meals under their own tab, and copies one into my library", async () => {
    const copy = makeMeal({ id: "copy1", name: "Soup" });
    const fake = fakeApi({
      "GET /partner": () => json(partnership()),
      "GET /meals": () => page([makeSummary()]),
      "GET /partner/meals": () => page([makeSummary({ id: "p1", name: "Soup" })]),
      "POST /meals/:id/copy": () => json(copy, 201),
    });
    renderWithClient(<MealsPage />);

    await userEvent.click(await screen.findByRole("tab", { name: "Partner's" }));
    expect(await screen.findByRole("link", { name: /Soup/ })).toHaveAttribute("href", "/meals/p1");
    expect(screen.queryByRole("link", { name: /Oat bowl/ })).not.toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: /Copy to my library/ }));
    await waitFor(() => expect(push).toHaveBeenCalledWith("/meals/copy1"));
    expect(fake.callsTo("POST", "/meals/p1/copy")).toHaveLength(1);
  });

  it("drops the partner tab, and shows my meals again, when the partner unlinks while it is open", async () => {
    let linked = true;
    fakeApi({
      "GET /partner": () => (linked ? json(partnership()) : problem(404, "partner_not_linked")),
      "GET /meals": () => page([makeSummary()]),
      "GET /partner/meals": () => problem(404, "partner_not_linked"),
    });
    renderWithClient(<MealsPage />);
    const tab = await screen.findByRole("tab", { name: "Partner's" });

    linked = false;
    await userEvent.click(tab);
    await waitFor(() => expect(screen.queryByRole("tab")).not.toBeInTheDocument());
    expect(await screen.findByRole("link", { name: /Oat bowl/ })).toBeInTheDocument();
  });
});
