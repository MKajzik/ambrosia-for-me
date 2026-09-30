// @vitest-environment jsdom
import { fireEvent, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ME_KEY } from "@/features/auth/use-me";
import { planKeys } from "@/features/plan/queries";
import { fakeApi, json, problem } from "@/test/fake-api";
import { renderWithClient } from "@/test/render";
import { makePlan } from "@/test/plan-fixtures";
import { makeUser } from "@/test/shopping-fixtures";
import { TargetsForm } from "./targets-form";

const toastError = vi.fn();
const toastSuccess = vi.fn();
vi.mock("sonner", () => ({ toast: { error: (m: string) => toastError(m), success: (m: string) => toastSuccess(m) } }));
afterEach(() => {
  toastError.mockReset();
  toastSuccess.mockReset();
});

const user = () => makeUser({ target_kcal: 2000, target_protein_g: 120, target_carbs_g: null, target_fat_g: 70 });
const patch = (fake: ReturnType<typeof fakeApi>) => fake.callsTo("PATCH", "/me");

describe("TargetsForm", () => {
  it("shows the targets the profile holds, blank where there is none", async () => {
    fakeApi({ "GET /me": () => json(user()) });
    renderWithClient(<TargetsForm />);
    expect(await screen.findByLabelText("Calories (kcal)")).toHaveValue("2000");
    expect(screen.getByLabelText("Protein (g)")).toHaveValue("120");
    expect(screen.getByLabelText("Carbohydrates (g)")).toHaveValue("");
    expect(screen.getByLabelText("Fat (g)")).toHaveValue("70");
  });

  it("saves all four, blank as null, then refreshes the profile and the plan's totals", async () => {
    const saved = makeUser({ target_kcal: 2000, target_protein_g: 120, target_carbs_g: 250, target_fat_g: null });
    const fake = fakeApi({ "GET /me": () => json(user()), "PATCH /me": () => json(saved) });
    const { queryClient } = renderWithClient(<TargetsForm />);
    queryClient.setQueryData(planKeys.range("2026-09-28", "2026-10-04"), makePlan("2026-09-28", "2026-10-04"));

    await userEvent.type(await screen.findByLabelText("Carbohydrates (g)"), "250");
    await userEvent.clear(screen.getByLabelText("Fat (g)"));
    await userEvent.click(screen.getByRole("button", { name: "Save targets" }));

    await waitFor(() => expect(patch(fake)).toHaveLength(1));
    expect(patch(fake)[0]?.body).toEqual({ target_kcal: 2000, target_protein_g: 120, target_carbs_g: 250, target_fat_g: null });
    await waitFor(() => expect(toastSuccess).toHaveBeenCalledWith("Targets saved."));
    expect(queryClient.getQueryData(ME_KEY)).toEqual(saved);
    expect(queryClient.getQueryState(planKeys.range("2026-09-28", "2026-10-04"))?.isInvalidated).toBe(true);
  });

  it("accepts zero for a macro and a comma in a number", async () => {
    const fake = fakeApi({ "GET /me": () => json(user()), "PATCH /me": () => json(user()) });
    renderWithClient(<TargetsForm />);
    fireEvent.change(await screen.findByLabelText("Protein (g)"), { target: { value: "0" } });
    fireEvent.change(screen.getByLabelText("Calories (kcal)"), { target: { value: "1850,5" } });
    await userEvent.click(screen.getByRole("button", { name: "Save targets" }));
    await waitFor(() => expect(patch(fake)).toHaveLength(1));
    expect(patch(fake)[0]?.body).toMatchObject({ target_kcal: 1850.5, target_protein_g: 0 });
  });

  it("refuses zero calories and text, names the fields, and sends nothing", async () => {
    const fake = fakeApi({ "GET /me": () => json(user()) });
    renderWithClient(<TargetsForm />);
    fireEvent.change(await screen.findByLabelText("Calories (kcal)"), { target: { value: "0" } });
    fireEvent.change(screen.getByLabelText("Fat (g)"), { target: { value: "lots" } });
    await userEvent.click(screen.getByRole("button", { name: "Save targets" }));
    expect(await screen.findByText("Calories must be more than 0 and at most 20000.")).toBeInTheDocument();
    expect(screen.getByText("Enter a number, for example 70.")).toBeInTheDocument();
    expect(patch(fake)).toHaveLength(0);
    expect(screen.getByLabelText("Fat (g)")).toHaveValue("lots");
  });

  it("explains a refusal in a toast and keeps what was typed", async () => {
    fakeApi({ "GET /me": () => json(user()), "PATCH /me": () => problem(500, "internal_error") });
    renderWithClient(<TargetsForm />);
    fireEvent.change(await screen.findByLabelText("Calories (kcal)"), { target: { value: "1800" } });
    await userEvent.click(screen.getByRole("button", { name: "Save targets" }));
    await waitFor(() => expect(toastError).toHaveBeenCalledWith("Something went wrong. Try again."));
    expect(screen.getByLabelText("Calories (kcal)")).toHaveValue("1800");
  });

  it("offers a retry when the profile cannot be loaded", async () => {
    let fail = true;
    fakeApi({ "GET /me": () => (fail ? problem(500, "internal_error") : json(user())) });
    renderWithClient(<TargetsForm />);
    expect(await screen.findByText("Something went wrong. Try again.")).toBeInTheDocument();
    fail = false;
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByLabelText("Calories (kcal)")).toHaveValue("2000");
  });
});
