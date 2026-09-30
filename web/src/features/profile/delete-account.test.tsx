// @vitest-environment jsdom
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { fakeApi, json, noContent, problem } from "@/test/fake-api";
import { renderWithClient } from "@/test/render";
import { makeUser } from "@/test/shopping-fixtures";
import { DeleteAccount } from "./delete-account";

const hardNavigate = vi.fn();
vi.mock("@/lib/navigation", () => ({ hardNavigate: (url: string) => hardNavigate(url) }));
afterEach(() => hardNavigate.mockReset());

const me = { "GET /me": () => json(makeUser({ email: "ann@example.test" })) };

async function openDialog() {
  const opener = await screen.findByRole("button", { name: "Delete account" });
  // The button stays off until the profile (and so the email to type) has loaded.
  await waitFor(() => expect(opener).toBeEnabled());
  await userEvent.click(opener);
  return screen.findByRole("dialog", { name: "Delete your account?" });
}

describe("DeleteAccount", () => {
  it("says what is lost before asking for anything, and keeps the button off until the email is typed", async () => {
    fakeApi({ ...me });
    renderWithClient(<DeleteAccount />);
    const dialog = await openDialog();
    expect(within(dialog).getByText(/permanently deletes your account/i)).toBeInTheDocument();
    expect(within(dialog).getByText(/meals, diet templates, plan and shopping lists/i)).toBeInTheDocument();
    const confirm = within(dialog).getByRole("button", { name: "Delete my account" });
    expect(confirm).toBeDisabled();
    await userEvent.type(within(dialog).getByLabelText(/Type ann@example\.test to confirm/), "ann@example.tes");
    expect(confirm).toBeDisabled();
    await userEvent.type(within(dialog).getByLabelText(/Type ann@example\.test to confirm/), "t");
    expect(confirm).toBeEnabled();
  });

  it("accepts the email in any case and with spaces round it", async () => {
    fakeApi({ ...me });
    renderWithClient(<DeleteAccount />);
    const dialog = await openDialog();
    await userEvent.type(within(dialog).getByLabelText(/to confirm/), "  ANN@Example.test ");
    expect(within(dialog).getByRole("button", { name: "Delete my account" })).toBeEnabled();
  });

  it("deletes the account, ends the session and leaves with a full page load", async () => {
    const fake = fakeApi({ ...me, "DELETE /me": () => noContent(), "POST /auth/logout": () => noContent() });
    renderWithClient(<DeleteAccount />);
    const dialog = await openDialog();
    await userEvent.type(within(dialog).getByLabelText(/to confirm/), "ann@example.test");
    await userEvent.click(within(dialog).getByRole("button", { name: "Delete my account" }));

    await waitFor(() => expect(hardNavigate).toHaveBeenCalledWith("/login"));
    expect(fake.callsTo("DELETE", "/me")).toHaveLength(1);
    expect(fake.callsTo("POST", "/auth/logout")).toHaveLength(1);
  });

  it("still leaves when signing out fails after the account is gone", async () => {
    fakeApi({
      ...me,
      "DELETE /me": () => noContent(),
      "POST /auth/logout": () => {
        throw new TypeError("offline");
      },
    });
    renderWithClient(<DeleteAccount />);
    const dialog = await openDialog();
    await userEvent.type(within(dialog).getByLabelText(/to confirm/), "ann@example.test");
    await userEvent.click(within(dialog).getByRole("button", { name: "Delete my account" }));
    await waitFor(() => expect(hardNavigate).toHaveBeenCalledWith("/login"));
  });

  it("keeps the session and says why when the delete fails", async () => {
    const fake = fakeApi({ ...me, "DELETE /me": () => problem(500, "internal_error") });
    renderWithClient(<DeleteAccount />);
    const dialog = await openDialog();
    await userEvent.type(within(dialog).getByLabelText(/to confirm/), "ann@example.test");
    await userEvent.click(within(dialog).getByRole("button", { name: "Delete my account" }));

    expect(await within(dialog).findByText("Something went wrong. Try again.")).toBeInTheDocument();
    expect(fake.callsTo("POST", "/auth/logout")).toHaveLength(0);
    expect(hardNavigate).not.toHaveBeenCalled();
    expect(screen.getByRole("dialog", { name: "Delete your account?" })).toBeInTheDocument();
  });

  it("does nothing when the person backs out", async () => {
    const fake = fakeApi({ ...me });
    renderWithClient(<DeleteAccount />);
    const dialog = await openDialog();
    await userEvent.click(within(dialog).getByRole("button", { name: "Keep my account" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(fake.callsTo("DELETE", "/me")).toHaveLength(0);
  });

  it("starts with an empty confirmation each time it is opened", async () => {
    fakeApi({ ...me });
    renderWithClient(<DeleteAccount />);
    let dialog = await openDialog();
    await userEvent.type(within(dialog).getByLabelText(/to confirm/), "ann@example.test");
    await userEvent.click(within(dialog).getByRole("button", { name: "Keep my account" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    dialog = await openDialog();
    expect(within(dialog).getByLabelText(/to confirm/)).toHaveValue("");
    expect(within(dialog).getByRole("button", { name: "Delete my account" })).toBeDisabled();
  });
});
