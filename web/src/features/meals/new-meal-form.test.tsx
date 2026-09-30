// @vitest-environment jsdom
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { fakeApi, json, problem } from "@/test/fake-api";
import { makeMeal } from "@/test/fixtures";
import { renderWithClient } from "@/test/render";
import { NewMealForm } from "./new-meal-form";

const replace = vi.fn();
vi.mock("next/navigation", () => ({ useRouter: () => ({ replace: (url: string) => replace(url), push: vi.fn() }) }));

afterEach(() => replace.mockReset());

describe("NewMealForm", () => {
  it("creates the meal and opens it in the editor", async () => {
    const fake = fakeApi({ "POST /meals": () => json(makeMeal({ id: "new1", name: "Curry" }), 201) });
    renderWithClient(<NewMealForm />);
    await userEvent.type(screen.getByLabelText("Name"), "Curry");
    await userEvent.clear(screen.getByLabelText("Servings"));
    await userEvent.type(screen.getByLabelText("Servings"), "4,5");
    await userEvent.click(screen.getByRole("button", { name: "Create meal" }));

    await waitFor(() => expect(replace).toHaveBeenCalledWith("/meals/new1"));
    expect(fake.callsTo("POST", "/meals")[0]?.body).toEqual({ name: "Curry", servings: 4.5 });
  });

  it("says what is missing and sends nothing", async () => {
    const fake = fakeApi({});
    renderWithClient(<NewMealForm />);
    await userEvent.clear(screen.getByLabelText("Servings"));
    await userEvent.click(screen.getByRole("button", { name: "Create meal" }));

    expect(await screen.findByText("Give the meal a name.")).toBeInTheDocument();
    expect(screen.getByText("Servings must be more than 0 and at most 1000.")).toBeInTheDocument();
    expect(fake.calls).toHaveLength(0);
  });

  it("stays on the form when the API refuses", async () => {
    fakeApi({ "POST /meals": () => problem(500, "internal_error") });
    renderWithClient(<NewMealForm />);
    await userEvent.type(screen.getByLabelText("Name"), "Curry");
    await userEvent.click(screen.getByRole("button", { name: "Create meal" }));
    await waitFor(() => expect(screen.getByRole("button", { name: "Create meal" })).toBeEnabled());
    expect(replace).not.toHaveBeenCalled();
  });
});
