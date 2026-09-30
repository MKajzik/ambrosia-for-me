// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ProfileSummary } from "./profile-summary";

const hardNavigate = vi.fn();
vi.mock("@/lib/navigation", () => ({ hardNavigate: (url: string) => hardNavigate(url) }));

const me = { id: "u1", email: "a@b.test", display_name: "Ann", created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z" };

function renderWith(ui: React.ReactElement) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>);
}

afterEach(() => {
  vi.unstubAllGlobals();
  hardNavigate.mockReset();
});

describe("ProfileSummary", () => {
  it("shows the signed-in user", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => Response.json(me)));
    renderWith(<ProfileSummary />);
    expect(await screen.findByText("Ann")).toBeInTheDocument();
    expect(screen.getByText("a@b.test")).toBeInTheDocument();
  });

  it("signs out: calls logout, then leaves with a full page load so no client state survives", async () => {
    const fetch = vi.fn(async (input: RequestInfo | URL) => (String(input).includes("logout") ? new Response(null, { status: 204 }) : Response.json(me)));
    vi.stubGlobal("fetch", fetch);
    renderWith(<ProfileSummary />);
    await screen.findByText("Ann");
    await userEvent.click(screen.getByRole("button", { name: "Sign out" }));

    await waitFor(() => expect(hardNavigate).toHaveBeenCalledWith("/login"));
    expect(fetch.mock.calls.some(([u]) => String(u).endsWith("/api/auth/logout"))).toBe(true);
  });

  it("still leaves the session behind when the logout call fails", async () => {
    vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL) => (String(input).includes("logout") ? Promise.reject(new TypeError("offline")) : Response.json(me))));
    renderWith(<ProfileSummary />);
    await screen.findByText("Ann");
    await userEvent.click(screen.getByRole("button", { name: "Sign out" }));
    await waitFor(() => expect(hardNavigate).toHaveBeenCalledWith("/login"));
  });
});
