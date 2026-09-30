// @vitest-environment jsdom
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { fakeApi, json, problem } from "@/test/fake-api";
import { makeTemplate, makeTemplateSlot } from "@/test/plan-fixtures";
import { renderWithClient } from "@/test/render";
import { TemplateDetail } from "./template-detail";

const push = vi.fn();
vi.mock("next/navigation", () => ({ useRouter: () => ({ push: (url: string) => push(url), replace: vi.fn() }) }));
afterEach(() => push.mockReset());

describe("TemplateDetail", () => {
  it("opens my own template in the editor", async () => {
    fakeApi({ "GET /diet-templates/:id": () => json(makeTemplate({ slots: [makeTemplateSlot()] })), "GET /partner": () => problem(404, "partner_not_linked") });
    renderWithClient(<TemplateDetail id="t1" />);
    expect(await screen.findByLabelText("Name", { exact: true })).toHaveValue("Base week");
    expect(screen.getByRole("heading", { name: "Edit template" })).toBeInTheDocument();
  });

  it("shows the partner's template read-only, with a copy button and no editor", async () => {
    const shared = makeTemplate({ id: "p1", name: "Bulk", is_owner: false, day_count: 1, slots: [makeTemplateSlot({ meal_name: "Stew" })] });
    const fake = fakeApi({
      "GET /diet-templates/:id": () => json(shared),
      "POST /diet-templates/:id/copy": () => json(makeTemplate({ id: "copy1", name: "Bulk" }), 201),
    });
    renderWithClient(<TemplateDetail id="p1" />);
    expect(await screen.findByRole("heading", { name: "Bulk" })).toBeInTheDocument();
    expect(screen.getByText("Stew")).toBeInTheDocument();
    expect(screen.queryByLabelText("Name", { exact: true })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Delete template" })).not.toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: /Copy to my library/ }));
    await waitFor(() => expect(push).toHaveBeenCalledWith("/plan/templates/copy1"));
    expect(fake.callsTo("POST", "/diet-templates/p1/copy")).toHaveLength(1);
  });

  it("says a template is not available, with a way back, when it is gone or no longer shared", async () => {
    fakeApi({ "GET /diet-templates/:id": () => problem(404, "not_found") });
    renderWithClient(<TemplateDetail id="gone" />);
    expect(await screen.findByText("That isn't available. It may have been removed, or it isn't shared with you.")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /Diet templates/ })).toHaveAttribute("href", "/plan/templates");
    expect(screen.queryByRole("button", { name: "Try again" })).not.toBeInTheDocument();
  });

  it("offers a retry when the template cannot be loaded for another reason", async () => {
    let fail = true;
    fakeApi({
      "GET /diet-templates/:id": () => (fail ? problem(500, "internal_error") : json(makeTemplate())),
      "GET /partner": () => problem(404, "partner_not_linked"),
    });
    renderWithClient(<TemplateDetail id="t1" />);
    expect(await screen.findByText("Something went wrong. Try again.")).toBeInTheDocument();
    fail = false;
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByLabelText("Name", { exact: true })).toBeInTheDocument();
  });
});
