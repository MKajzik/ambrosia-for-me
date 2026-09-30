// @vitest-environment jsdom
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { fakeApi, json, problem } from "@/test/fake-api";
import { makeTemplate } from "@/test/plan-fixtures";
import { renderWithClient } from "@/test/render";
import { NewTemplateForm } from "./new-template-form";

const replace = vi.fn();
vi.mock("next/navigation", () => ({ useRouter: () => ({ replace: (url: string) => replace(url), push: vi.fn() }) }));
afterEach(() => replace.mockReset());

describe("NewTemplateForm", () => {
  it("creates a week-long template by default and opens it in the editor", async () => {
    const fake = fakeApi({ "POST /diet-templates": () => json(makeTemplate({ id: "new1", name: "Cut" }), 201) });
    renderWithClient(<NewTemplateForm />);
    expect(screen.getByLabelText("Number of days")).toHaveValue("7");
    await userEvent.type(screen.getByLabelText("Name"), "Cut");
    await userEvent.click(screen.getByRole("button", { name: "Create template" }));

    await waitFor(() => expect(replace).toHaveBeenCalledWith("/plan/templates/new1"));
    expect(fake.callsTo("POST", "/diet-templates")[0]?.body).toEqual({ name: "Cut", day_count: 7 });
  });

  it("takes any length from 1 to 31 days", async () => {
    const fake = fakeApi({ "POST /diet-templates": () => json(makeTemplate({ id: "n2" }), 201) });
    renderWithClient(<NewTemplateForm />);
    await userEvent.type(screen.getByLabelText("Name"), "Two days");
    await userEvent.clear(screen.getByLabelText("Number of days"));
    await userEvent.type(screen.getByLabelText("Number of days"), "31");
    await userEvent.click(screen.getByRole("button", { name: "Create template" }));
    await waitFor(() => expect(fake.callsTo("POST", "/diet-templates")).toHaveLength(1));
    expect(fake.callsTo("POST", "/diet-templates")[0]?.body).toEqual({ name: "Two days", day_count: 31 });
  });

  it.each(["0", "32", "abc", "2.5", "", "-1"])("refuses %j days, says what is allowed, and sends nothing", async (days) => {
    const fake = fakeApi({});
    renderWithClient(<NewTemplateForm />);
    await userEvent.type(screen.getByLabelText("Name"), "Cut");
    await userEvent.clear(screen.getByLabelText("Number of days"));
    if (days) await userEvent.type(screen.getByLabelText("Number of days"), days);
    await userEvent.click(screen.getByRole("button", { name: "Create template" }));
    expect(await screen.findByText("Days must be a whole number from 1 to 31.")).toBeInTheDocument();
    expect(fake.calls).toHaveLength(0);
  });

  it("asks for a name", async () => {
    const fake = fakeApi({});
    renderWithClient(<NewTemplateForm />);
    await userEvent.click(screen.getByRole("button", { name: "Create template" }));
    expect(await screen.findByText("Give the template a name.")).toBeInTheDocument();
    expect(fake.calls).toHaveLength(0);
  });

  it("stays on the form when the API refuses", async () => {
    fakeApi({ "POST /diet-templates": () => problem(500, "internal_error") });
    renderWithClient(<NewTemplateForm />);
    await userEvent.type(screen.getByLabelText("Name"), "Cut");
    await userEvent.click(screen.getByRole("button", { name: "Create template" }));
    await waitFor(() => expect(screen.getByRole("button", { name: "Create template" })).toBeEnabled());
    expect(replace).not.toHaveBeenCalled();
  });
});
