// @vitest-environment jsdom
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { PARTNER_KEY } from "@/features/meals/queries";
import { fakeApi, json, problem } from "@/test/fake-api";
import { partnership } from "@/test/fixtures";
import { makeTemplate, makeTemplateSummary } from "@/test/plan-fixtures";
import { renderWithClient } from "@/test/render";
import { TemplatesPage } from "./templates-page";

const push = vi.fn();
vi.mock("next/navigation", () => ({ useRouter: () => ({ push: (url: string) => push(url), replace: vi.fn() }) }));
afterEach(() => push.mockReset());

const page = (items: ReturnType<typeof makeTemplateSummary>[], next_cursor: string | null = null) => json({ items, next_cursor });

async function partnerSettled(queryClient: ReturnType<typeof renderWithClient>["queryClient"]) {
  await waitFor(() => expect(queryClient.getQueryState(PARTNER_KEY)?.status).toBe("success"));
}

describe("TemplatesPage", () => {
  it("lists my templates with their length, a way to make one, and no tabs without a partner", async () => {
    fakeApi({
      "GET /partner": () => problem(404, "partner_not_linked"),
      "GET /diet-templates": () => page([makeTemplateSummary({ shared_with_partner: true }), makeTemplateSummary({ id: "t2", name: "Cut", day_count: 1 })]),
    });
    const { queryClient } = renderWithClient(<TemplatesPage />);

    expect(await screen.findByRole("link", { name: /Base week/ })).toHaveAttribute("href", "/plan/templates/t1");
    expect(screen.getByRole("link", { name: /Base week/ })).toHaveTextContent("7 days");
    expect(screen.getByRole("link", { name: /Cut/ })).toHaveTextContent("1 day");
    expect(screen.getByText("Shared")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /New template/ })).toHaveAttribute("href", "/plan/templates/new");
    expect(screen.getByRole("link", { name: /Plan/ })).toHaveAttribute("href", "/plan");
    await partnerSettled(queryClient);
    expect(screen.queryByRole("tab")).not.toBeInTheDocument();
  });

  it("asks for the next page with the API's cursor", async () => {
    const fake = fakeApi({
      "GET /partner": () => problem(404, "partner_not_linked"),
      "GET /diet-templates": (req) => (req.search.get("cursor") === "c1" ? page([makeTemplateSummary({ id: "t2", name: "Cut" })]) : page([makeTemplateSummary()], "c1")),
    });
    renderWithClient(<TemplatesPage />);
    await userEvent.click(await screen.findByRole("button", { name: "Load more" }));
    expect(await screen.findByRole("link", { name: /Cut/ })).toBeInTheDocument();
    expect(fake.callsTo("GET", "/diet-templates")[1]?.search.get("cursor")).toBe("c1");
  });

  it("invites you to make your first template when there are none", async () => {
    fakeApi({ "GET /partner": () => problem(404, "partner_not_linked"), "GET /diet-templates": () => page([]) });
    renderWithClient(<TemplatesPage />);
    expect(await screen.findByText("No diet templates yet")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Create your first template" })).toHaveAttribute("href", "/plan/templates/new");
  });

  it("shows the partner's shared templates under their own tab, and copies one into my library", async () => {
    const copy = makeTemplate({ id: "copy1", name: "Bulk" });
    const fake = fakeApi({
      "GET /partner": () => json(partnership()),
      "GET /diet-templates": () => page([makeTemplateSummary()]),
      "GET /partner/diet-templates": () => page([makeTemplateSummary({ id: "p1", name: "Bulk" })]),
      "POST /diet-templates/:id/copy": () => json(copy, 201),
    });
    renderWithClient(<TemplatesPage />);

    await userEvent.click(await screen.findByRole("tab", { name: "Partner's" }));
    expect(await screen.findByRole("link", { name: /Bulk/ })).toHaveAttribute("href", "/plan/templates/p1");
    expect(screen.queryByRole("link", { name: /Base week/ })).not.toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: /Copy to my library/ }));
    await waitFor(() => expect(push).toHaveBeenCalledWith("/plan/templates/copy1"));
    expect(fake.callsTo("POST", "/diet-templates/p1/copy")).toHaveLength(1);
  });

  it("drops the partner tab, and shows my templates again, when the partner unlinks while it is open", async () => {
    let linked = true;
    fakeApi({
      "GET /partner": () => (linked ? json(partnership()) : problem(404, "partner_not_linked")),
      "GET /diet-templates": () => page([makeTemplateSummary()]),
      "GET /partner/diet-templates": () => problem(404, "partner_not_linked"),
    });
    renderWithClient(<TemplatesPage />);
    const tab = await screen.findByRole("tab", { name: "Partner's" });

    linked = false;
    await userEvent.click(tab);
    await waitFor(() => expect(screen.queryByRole("tab")).not.toBeInTheDocument());
    expect(await screen.findByRole("link", { name: /Base week/ })).toBeInTheDocument();
  });
});
