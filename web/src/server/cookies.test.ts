import { afterEach, describe, expect, it, vi } from "vitest";
import { ACCESS_COOKIE, REFRESH_COOKIE, clearSessionCookies, setSessionCookies } from "./cookies";

function recorder() {
  const calls: { name: string; value: string; options: Record<string, unknown> }[] = [];
  return { calls, set: (name: string, value: string, options: Record<string, unknown>) => void calls.push({ name, value, options }) };
}

afterEach(() => vi.unstubAllEnvs());

describe("session cookies", () => {
  it("sets both cookies httpOnly and lax, with the access cookie under /api and as long-lived as the token", () => {
    vi.stubEnv("COOKIE_SECURE", "true");
    const jar = recorder();
    setSessionCookies(jar, { accessToken: "a1", refreshToken: "r1", expiresIn: 900 });
    expect(jar.calls).toEqual([
      { name: ACCESS_COOKIE, value: "a1", options: { httpOnly: true, secure: true, sameSite: "lax", path: "/api", maxAge: 900 } },
      { name: REFRESH_COOKIE, value: "r1", options: { httpOnly: true, secure: true, sameSite: "lax", path: "/", maxAge: 2592000 } },
    ]);
  });

  it("is Secure by default in production and not in development", () => {
    const jar = recorder();
    vi.stubEnv("COOKIE_SECURE", "");
    vi.stubEnv("NODE_ENV", "production");
    setSessionCookies(jar, { accessToken: "a", refreshToken: "r", expiresIn: 1 });
    expect(jar.calls[0]?.options.secure).toBe(true);
    vi.stubEnv("NODE_ENV", "development");
    jar.calls.length = 0;
    setSessionCookies(jar, { accessToken: "a", refreshToken: "r", expiresIn: 1 });
    expect(jar.calls[0]?.options.secure).toBe(false);
  });

  it("COOKIE_SECURE=false overrides production (plain-http local stacks)", () => {
    vi.stubEnv("NODE_ENV", "production");
    vi.stubEnv("COOKIE_SECURE", "false");
    const jar = recorder();
    setSessionCookies(jar, { accessToken: "a", refreshToken: "r", expiresIn: 1 });
    expect(jar.calls.every((c) => c.options.secure === false)).toBe(true);
  });

  it("clears both cookies on the same paths they were set on", () => {
    const jar = recorder();
    clearSessionCookies(jar);
    expect(jar.calls.map((c) => [c.name, c.value, c.options.path, c.options.maxAge])).toEqual([
      [ACCESS_COOKIE, "", "/api", 0],
      [REFRESH_COOKIE, "", "/", 0],
    ]);
  });
});
