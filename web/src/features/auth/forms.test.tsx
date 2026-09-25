// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { LoginForm } from "./login-form";
import { RegisterForm } from "./register-form";
import { ME_KEY } from "./use-me";

const replace = vi.fn();
vi.mock("next/navigation", () => ({ useRouter: () => ({ replace }) }));

const user = { id: "u1", email: "a@b.test", display_name: "Ann" };
let queryClient: QueryClient;

function renderWith(ui: React.ReactElement) {
  queryClient = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(<QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>);
}

function stubFetch(res: () => Response) {
  const fn = vi.fn(async (_url: string, _init?: RequestInit) => res());
  vi.stubGlobal("fetch", fn);
  return fn;
}
const problem = (status: number, body: object, headers: Record<string, string> = {}) =>
  Response.json(body, { status, headers: { "Content-Type": "application/problem+json", ...headers } });

beforeEach(() => replace.mockReset());
afterEach(() => vi.unstubAllGlobals());

describe("LoginForm", () => {
  it("submits the credentials, caches the user and goes to the requested page", async () => {
    const fetch = stubFetch(() => Response.json({ user }));
    renderWith(<LoginForm next="/meals?tab=partner" />);
    await userEvent.type(screen.getByLabelText("Email"), "a@b.test");
    await userEvent.type(screen.getByLabelText("Password"), "hunter2hunter2");
    await userEvent.click(screen.getByRole("button", { name: "Sign in" }));

    await waitFor(() => expect(replace).toHaveBeenCalledWith("/meals?tab=partner"));
    expect(fetch.mock.calls[0]![1]).toMatchObject({ body: '{"email":"a@b.test","password":"hunter2hunter2"}' });
    expect(queryClient.getQueryData(ME_KEY)).toEqual(user);
  });

  it("says so, and stays put, when the credentials are wrong", async () => {
    stubFetch(() => problem(401, { code: "invalid_credentials" }));
    renderWith(<LoginForm next="/today" />);
    await userEvent.type(screen.getByLabelText("Email"), "a@b.test");
    await userEvent.type(screen.getByLabelText("Password"), "wrong");
    await userEvent.click(screen.getByRole("button", { name: "Sign in" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("That email and password don't match.");
    expect(replace).not.toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "Sign in" })).toBeEnabled();
  });

  it("tells the person how long to wait when rate limited", async () => {
    stubFetch(() => problem(429, { code: "rate_limited" }, { "Retry-After": "42" }));
    renderWith(<LoginForm next="/today" />);
    await userEvent.click(screen.getByRole("button", { name: "Sign in" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Try again in 42 seconds.");
  });

  it("disables the button while the request is in flight", async () => {
    let finish!: (r: Response) => void;
    stubFetch(() => new Promise<Response>((resolve) => (finish = resolve)) as unknown as Response);
    renderWith(<LoginForm next="/today" />);
    await userEvent.click(screen.getByRole("button", { name: "Sign in" }));
    expect(await screen.findByRole("button", { name: "Signing in…" })).toBeDisabled();
    finish(Response.json({ user }));
    await waitFor(() => expect(replace).toHaveBeenCalled());
  });
});

describe("RegisterForm", () => {
  it("creates the account and lands on Today", async () => {
    const fetch = stubFetch(() => Response.json({ user }, { status: 201 }));
    renderWith(<RegisterForm />);
    await userEvent.type(screen.getByLabelText("Your name"), "Ann");
    await userEvent.type(screen.getByLabelText("Email"), "a@b.test");
    await userEvent.type(screen.getByLabelText("Password"), "long-enough-pw");
    await userEvent.click(screen.getByRole("button", { name: "Create account" }));

    await waitFor(() => expect(replace).toHaveBeenCalledWith("/today"));
    expect(JSON.parse(String(fetch.mock.calls[0]![1]?.body))).toEqual({ display_name: "Ann", email: "a@b.test", password: "long-enough-pw" });
  });

  it("shows each field's problem next to that field and marks it invalid", async () => {
    stubFetch(() =>
      problem(400, {
        code: "validation_failed",
        errors: [
          { field: "email", code: "invalid_format" },
          { field: "password", code: "too_short" },
        ],
      }),
    );
    renderWith(<RegisterForm />);
    await userEvent.click(screen.getByRole("button", { name: "Create account" }));

    const email = await screen.findByLabelText("Email");
    expect(email).toHaveAttribute("aria-invalid", "true");
    expect(email).toHaveAccessibleDescription("Enter a valid email address.");
    expect(screen.getByLabelText("Password")).toHaveAccessibleDescription("At least 10 characters. Use at least 10 characters.");
    expect(screen.getByRole("alert")).toHaveTextContent("Check the highlighted fields.");
  });

  it("puts a taken email at the email field, not in a banner", async () => {
    stubFetch(() => problem(409, { code: "email_taken" }));
    renderWith(<RegisterForm />);
    await userEvent.click(screen.getByRole("button", { name: "Create account" }));

    expect(await screen.findByLabelText("Email")).toHaveAccessibleDescription("An account with that email already exists.");
    expect(screen.queryByRole("alert")).toBeNull();
  });
});
