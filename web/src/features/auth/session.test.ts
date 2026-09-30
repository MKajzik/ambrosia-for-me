import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "@/lib/api/problem";
import { login, logout, register } from "./session";

const user = { id: "u1", email: "a@b.test", display_name: "Ann" };

function stubFetch(res: () => Response) {
  const fn = vi.fn(async (_url: string, _init?: RequestInit) => res());
  vi.stubGlobal("fetch", fn);
  return fn;
}

afterEach(() => vi.unstubAllGlobals());

describe("session helpers", () => {
  it("logs in with a JSON POST and returns the user", async () => {
    const fetch = stubFetch(() => Response.json({ user }));
    await expect(login({ email: "a@b.test", password: "pw" })).resolves.toEqual(user);
    const [url, init] = fetch.mock.calls[0]!;
    expect(url).toBe("/api/auth/login");
    expect(init).toMatchObject({ method: "POST", headers: { "Content-Type": "application/json" }, body: '{"email":"a@b.test","password":"pw"}' });
  });

  it("registers through /api/auth/register", async () => {
    const fetch = stubFetch(() => Response.json({ user }, { status: 201 }));
    await register({ email: "a@b.test", password: "long-enough-pw", display_name: "Ann" });
    expect(fetch.mock.calls[0]![0]).toBe("/api/auth/register");
  });

  it("throws an ApiError with field messages when the API refuses", async () => {
    stubFetch(() => Response.json({ code: "validation_failed", errors: [{ field: "password", code: "too_short" }] }, { status: 400 }));
    const err = await register({ email: "a@b.test", password: "short", display_name: "Ann" }).catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect(err).toMatchObject({ code: "validation_failed", fieldErrors: { password: "Use at least 10 characters." } });
  });

  it("logs out with a bodiless POST", async () => {
    const fetch = stubFetch(() => new Response(null, { status: 204 }));
    await logout();
    expect(fetch.mock.calls[0]).toEqual(["/api/auth/logout", { method: "POST", headers: undefined, body: undefined }]);
  });
});
