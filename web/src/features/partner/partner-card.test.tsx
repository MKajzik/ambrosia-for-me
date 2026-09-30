// @vitest-environment jsdom
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { PARTNER_KEY, mealKeys } from "@/features/meals/queries";
import { templateKeys } from "@/features/plan/template-queries";
import { shoppingKeys } from "@/features/shopping/queries";
import { fakeApi, json, noContent, problem } from "@/test/fake-api";
import { partnership } from "@/test/fixtures";
import { renderWithClient } from "@/test/render";
import { PartnerCard } from "./partner-card";

const toastError = vi.fn();
const toastSuccess = vi.fn();
vi.mock("sonner", () => ({ toast: { error: (m: string) => toastError(m), success: (m: string) => toastSuccess(m) } }));
afterEach(() => {
  toastError.mockReset();
  toastSuccess.mockReset();
});

function setClipboard(writeText: (text: string) => Promise<void>) {
  Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
}

const noPartner = () => problem(404, "partner_not_linked");
const linked = () => ({ ...partnership(), linked_at: "2026-01-15T12:00:00Z" });
const pending = () => ({ ...partnership("pending"), expires_at: "2026-02-01T12:00:00Z" });

describe("with no partner", () => {
  it("offers to create a code or enter one", async () => {
    fakeApi({ "GET /partner": noPartner });
    renderWithClient(<PartnerCard />);
    expect(await screen.findByRole("button", { name: "Create invite code" })).toBeInTheDocument();
    expect(screen.getByLabelText("Invite code")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Link accounts" })).toBeInTheDocument();
  });

  it("shows a new code once, in groups, with its expiry, and keeps showing it while the invite is pending", async () => {
    let server: "none" | "pending" = "none";
    const fake = fakeApi({
      "GET /partner": () => (server === "none" ? noPartner() : json(pending())),
      "POST /partner/invite": () => {
        server = "pending";
        return json({ code: "ABCDEFGH", expires_at: "2026-02-01T12:00:00Z" }, 201);
      },
    });
    renderWithClient(<PartnerCard />);
    await userEvent.click(await screen.findByRole("button", { name: "Create invite code" }));

    expect(await screen.findByText("ABCD-EFGH")).toBeInTheDocument();
    expect(screen.getByText(/Shown once\. Share it with your partner\./)).toBeInTheDocument();
    expect(await screen.findByText(/Waiting for your partner\. The code expires Feb 1, 2026/)).toBeInTheDocument();
    expect(screen.getByText("ABCD-EFGH")).toBeInTheDocument();
    expect(fake.callsTo("POST", "/partner/invite")).toHaveLength(1);
  });

  it("copies the code, without the dashes", async () => {
    const writeText = vi.fn(async (_text: string) => undefined);
    setClipboard(writeText);
    fakeApi({ "GET /partner": noPartner, "POST /partner/invite": () => json({ code: "ABCDEFGH", expires_at: "2026-02-01T12:00:00Z" }, 201) });
    renderWithClient(<PartnerCard />);
    await userEvent.click(await screen.findByRole("button", { name: "Create invite code" }));
    await userEvent.click(await screen.findByRole("button", { name: "Copy code" }));
    expect(writeText).toHaveBeenCalledWith("ABCDEFGH");
    await waitFor(() => expect(toastSuccess).toHaveBeenCalledWith("Code copied."));
  });

  it("tells the person to copy by hand, and does not crash, when the clipboard refuses", async () => {
    setClipboard(async () => Promise.reject(new Error("denied")));
    fakeApi({ "GET /partner": noPartner, "POST /partner/invite": () => json({ code: "ABCDEFGH", expires_at: "2026-02-01T12:00:00Z" }, 201) });
    renderWithClient(<PartnerCard />);
    await userEvent.click(await screen.findByRole("button", { name: "Create invite code" }));
    await userEvent.click(await screen.findByRole("button", { name: "Copy code" }));
    await waitFor(() => expect(toastError).toHaveBeenCalledWith("Couldn't copy. Select the code and copy it by hand."));
    expect(screen.getByText("ABCD-EFGH")).toBeInTheDocument();
  });

  it("links with a code, sending it trimmed, and shows the partnership", async () => {
    let server: "none" | "linked" = "none";
    const fake = fakeApi({
      "GET /partner": () => (server === "none" ? noPartner() : json(linked())),
      "POST /partner/accept": () => {
        server = "linked";
        return json(linked());
      },
    });
    renderWithClient(<PartnerCard />);
    await userEvent.type(await screen.findByLabelText("Invite code"), "  k7m2-qx9r ");
    await userEvent.click(screen.getByRole("button", { name: "Link accounts" }));

    expect(await screen.findByText(/Linked with Sam since Jan 15, 2026\./)).toBeInTheDocument();
    expect(fake.callsTo("POST", "/partner/accept")[0]?.body).toEqual({ code: "k7m2-qx9r" });
    expect(toastSuccess).toHaveBeenCalledWith("Linked with Sam.");
  });

  it.each([
    ["a wrong or used code", 404, "invite_invalid", "That invite code isn't valid. It may have expired or already been used."],
    ["already being linked", 409, "partner_already_linked", "You're already linked with a partner."],
  ])("shows %s on the code field, not as a toast", async (_what, status, code, message) => {
    fakeApi({ "GET /partner": noPartner, "POST /partner/accept": () => problem(status, code) });
    renderWithClient(<PartnerCard />);
    await userEvent.type(await screen.findByLabelText("Invite code"), "WRONGCODE");
    await userEvent.click(screen.getByRole("button", { name: "Link accounts" }));
    expect(await screen.findByText(message)).toBeInTheDocument();
    expect(toastError).not.toHaveBeenCalled();
  });

  it("asks for the code and sends nothing when the field is blank", async () => {
    const fake = fakeApi({ "GET /partner": noPartner });
    renderWithClient(<PartnerCard />);
    await userEvent.click(await screen.findByRole("button", { name: "Link accounts" }));
    expect(await screen.findByText("Enter the code your partner sent you.")).toBeInTheDocument();
    expect(fake.callsTo("POST", "/partner/accept")).toHaveLength(0);
  });

  it("toasts any other failure", async () => {
    fakeApi({ "GET /partner": noPartner, "POST /partner/accept": () => problem(429, "rate_limited") });
    renderWithClient(<PartnerCard />);
    await userEvent.type(await screen.findByLabelText("Invite code"), "ABCDEFGH");
    await userEvent.click(screen.getByRole("button", { name: "Link accounts" }));
    await waitFor(() => expect(toastError).toHaveBeenCalledWith("Too many attempts. Try again in a minute."));
  });
});

describe("with a pending invite", () => {
  it("says when it expires, offers a new code and cancelling, and cancels", async () => {
    let server: "pending" | "none" = "pending";
    const fake = fakeApi({
      "GET /partner": () => (server === "pending" ? json(pending()) : noPartner()),
      "DELETE /partner": () => {
        server = "none";
        return noContent();
      },
    });
    renderWithClient(<PartnerCard />);
    expect(await screen.findByText(/Waiting for your partner/)).toBeInTheDocument();
    expect(screen.getByText(/Feb 1, 2026/)).toBeInTheDocument();
    expect(screen.getByText(/The code was shown when you created it/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Create a new code" })).toBeInTheDocument();
    expect(screen.getByLabelText("Invite code")).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "Cancel invite" }));
    expect(await screen.findByRole("button", { name: "Create invite code" })).toBeInTheDocument();
    expect(fake.callsTo("DELETE", "/partner")).toHaveLength(1);
  });
});

describe("when linked", () => {
  it("shows who and since when", async () => {
    fakeApi({ "GET /partner": () => json(linked()) });
    renderWithClient(<PartnerCard />);
    expect(await screen.findByText(/Linked with Sam since Jan 15, 2026\./)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Create invite code" })).not.toBeInTheDocument();
  });

  it("says what ends before unlinking, and unlinks only when confirmed, dropping what was the partner's from the cache", async () => {
    let server: "linked" | "none" = "linked";
    const fake = fakeApi({
      "GET /partner": () => (server === "linked" ? json(linked()) : noPartner()),
      "DELETE /partner": () => {
        server = "none";
        return noContent();
      },
    });
    const { queryClient } = renderWithClient(<PartnerCard />);
    queryClient.setQueryData(mealKeys.partner, { pages: [], pageParams: [] });
    queryClient.setQueryData(templateKeys.partner, { pages: [], pageParams: [] });
    queryClient.setQueryData(shoppingKeys.partner, { pages: [], pageParams: [] });

    await userEvent.click(await screen.findByRole("button", { name: "Unlink" }));
    const dialog = await screen.findByRole("dialog", { name: "Unlink from Sam?" });
    expect(within(dialog).getByText(/stop seeing each other's shared meals, diet templates and shopping lists/i)).toBeInTheDocument();
    expect(within(dialog).getByText(/Copies stay with whoever made them/i)).toBeInTheDocument();
    expect(within(dialog).getByText(/need a new invite to link again/i)).toBeInTheDocument();
    expect(fake.callsTo("DELETE", "/partner")).toHaveLength(0);

    await userEvent.click(within(dialog).getByRole("button", { name: "Unlink" }));
    expect(await screen.findByRole("button", { name: "Create invite code" })).toBeInTheDocument();
    expect(fake.callsTo("DELETE", "/partner")).toHaveLength(1);
    expect(queryClient.getQueryData(PARTNER_KEY)).toBeNull();
    expect(queryClient.getQueryData(mealKeys.partner)).toBeUndefined();
    expect(queryClient.getQueryData(templateKeys.partner)).toBeUndefined();
    expect(queryClient.getQueryData(shoppingKeys.partner)).toBeUndefined();
  });

  it("does nothing when the person backs out, and says why when the unlink fails", async () => {
    let fail = true;
    const fake = fakeApi({ "GET /partner": () => json(linked()), "DELETE /partner": () => (fail ? problem(500, "internal_error") : noContent()) });
    renderWithClient(<PartnerCard />);
    await userEvent.click(await screen.findByRole("button", { name: "Unlink" }));
    let dialog = await screen.findByRole("dialog", { name: "Unlink from Sam?" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Keep the link" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(fake.callsTo("DELETE", "/partner")).toHaveLength(0);

    await userEvent.click(screen.getByRole("button", { name: "Unlink" }));
    dialog = await screen.findByRole("dialog", { name: "Unlink from Sam?" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Unlink" }));
    expect(await within(dialog).findByText("Something went wrong. Try again.")).toBeInTheDocument();
    expect(screen.getByRole("dialog", { name: "Unlink from Sam?" })).toBeInTheDocument();
    fail = false;
  });
});

describe("loading the partner link", () => {
  it("offers a retry when it fails", async () => {
    let fail = true;
    fakeApi({ "GET /partner": () => (fail ? problem(500, "internal_error") : noPartner()) });
    renderWithClient(<PartnerCard />);
    expect(await screen.findByText("Something went wrong. Try again.")).toBeInTheDocument();
    fail = false;
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByRole("button", { name: "Create invite code" })).toBeInTheDocument();
  });
});
