import { NextRequest } from "next/server";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { login, logout, register } from "./auth";

type Fetch = (url: string, init: RequestInit & { headers: Headers }) => Promise<Response>;
function stubFetch(impl: Fetch) {
  const fn = vi.fn(impl);
  vi.stubGlobal("fetch", fn);
  return fn;
}

function post(path: string, body: string | null, extra: Record<string, string> = {}, cookie?: string) {
  const headers: Record<string, string> = { host: "app.test", origin: "http://app.test", ...extra };
  if (body !== null) headers["content-type"] = "application/json";
  if (cookie) headers.cookie = cookie;
  return new NextRequest(`http://app.test/api/auth/${path}`, { method: "POST", headers, body });
}

const authBody = {
  access_token: "ACCESS-SECRET",
  refresh_token: "REFRESH-SECRET",
  token_type: "Bearer",
  expires_in: 900,
  user: { id: "u1", email: "a@b.test", display_name: "Ann" },
};

beforeEach(() => vi.stubEnv("API_BASE_URL", "http://api.test/v1"));
afterEach(() => vi.unstubAllEnvs());

describe("login and register", () => {
  it("sets the session cookies and returns only the user, never a token", async () => {
    const fetch = stubFetch(async () => Response.json(authBody));
    const res = await login(post("login", '{"email":"a@b.test","password":"pw"}'));
    expect(res.status).toBe(200);
    const text = await res.text();
    expect(JSON.parse(text)).toEqual({ user: authBody.user });
    expect(text).not.toContain("SECRET");
    const cookies = res.headers.getSetCookie().join("\n");
    expect(cookies).toMatch(/mp_access=ACCESS-SECRET;.*HttpOnly/);
    expect(cookies).toMatch(/mp_refresh=REFRESH-SECRET;.*HttpOnly/);
    const [url, init] = fetch.mock.calls[0]!;
    expect(url).toBe("http://api.test/v1/auth/login");
    expect(init.headers.get("authorization")).toBeNull();
    expect(new TextDecoder().decode(init.body as ArrayBuffer)).toBe('{"email":"a@b.test","password":"pw"}');
  });

  it("keeps the API's 201 for a registration", async () => {
    const fetch = stubFetch(async () => Response.json(authBody, { status: 201 }));
    const res = await register(post("register", '{"email":"a@b.test","password":"long-enough-pw","display_name":"Ann"}'));
    expect(res.status).toBe(201);
    expect(fetch.mock.calls[0]![0]).toBe("http://api.test/v1/auth/register");
  });

  it("relays a failure as the API's problem, with no cookies", async () => {
    stubFetch(async () =>
      new Response(JSON.stringify({ code: "validation_failed", errors: [{ field: "email", code: "invalid_format" }] }), {
        status: 400,
        headers: { "Content-Type": "application/problem+json" },
      }),
    );
    const res = await register(post("register", "{}"));
    expect(res.status).toBe(400);
    expect(await res.json()).toMatchObject({ code: "validation_failed", errors: [{ field: "email" }] });
    expect(res.headers.getSetCookie()).toEqual([]);
  });

  it("relays a rate limit with its Retry-After", async () => {
    stubFetch(async () => new Response("{}", { status: 429, headers: { "Content-Type": "application/problem+json", "Retry-After": "30" } }));
    const res = await login(post("login", "{}"));
    expect(res.status).toBe(429);
    expect(res.headers.get("retry-after")).toBe("30");
  });

  it("refuses a cross-origin attempt without calling the API", async () => {
    const fetch = stubFetch(async () => Response.json(authBody));
    const res = await login(post("login", "{}", { origin: "http://evil.test" }));
    expect(res.status).toBe(403);
    expect(fetch).not.toHaveBeenCalled();
  });

  it("answers 502 when the API cannot be reached", async () => {
    stubFetch(async () => Promise.reject(new TypeError("fetch failed")));
    expect((await login(post("login", "{}"))).status).toBe(502);
  });

  it("does not let a client choose its own address for the API's limiter", async () => {
    const fetch = stubFetch(async () => Response.json(authBody));
    await login(post("login", "{}", { "x-forwarded-for": "6.6.6.6" }));
    expect(fetch.mock.calls[0]![1].headers.get("x-forwarded-for")).toBeNull();
  });
});

describe("logout", () => {
  it("revokes the session at the API and clears both cookies", async () => {
    const fetch = stubFetch(async () => new Response(null, { status: 204 }));
    const res = await logout(post("logout", null, {}, "mp_refresh=r1; mp_access=a1"));
    expect(res.status).toBe(204);
    const [url, init] = fetch.mock.calls[0]!;
    expect(url).toBe("http://api.test/v1/auth/logout");
    expect(String(init.body)).toBe('{"refresh_token":"r1"}');
    const cookies = res.headers.getSetCookie().join("\n");
    expect(cookies).toMatch(/mp_access=;.*Max-Age=0/);
    expect(cookies).toMatch(/mp_refresh=;.*Max-Age=0/);
  });

  it("still signs the browser out when the API is down", async () => {
    stubFetch(async () => Promise.reject(new TypeError("fetch failed")));
    const res = await logout(post("logout", null, {}, "mp_refresh=r1"));
    expect(res.status).toBe(204);
    expect(res.headers.getSetCookie().join("\n")).toMatch(/mp_refresh=;.*Max-Age=0/);
  });

  it("does not call the API when there is no session", async () => {
    const fetch = stubFetch(async () => new Response(null, { status: 204 }));
    const res = await logout(post("logout", null));
    expect(res.status).toBe(204);
    expect(fetch).not.toHaveBeenCalled();
  });

  it("refuses a cross-origin logout", async () => {
    const res = await logout(post("logout", null, { origin: "http://evil.test" }, "mp_refresh=r1"));
    expect(res.status).toBe(403);
  });
});

describe("body limit", () => {
  const CHUNK = 16 * 1024;
  /** A chunked request body: no Content-Length, and it counts how much of it was pulled. */
  function streamedLogin(chunks: number) {
    const state = { pulled: 0, cancelled: false };
    const stream = new ReadableStream<Uint8Array>({
      pull(controller) {
        if (state.pulled >= chunks) return controller.close();
        state.pulled++;
        controller.enqueue(new Uint8Array(CHUNK).fill(120));
      },
      cancel() {
        state.cancelled = true;
      },
    });
    const req = new NextRequest("http://app.test/api/auth/login", {
      method: "POST",
      headers: { host: "app.test", origin: "http://app.test", "content-type": "application/json" },
      body: stream,
      duplex: "half",
    } as ConstructorParameters<typeof NextRequest>[1]);
    expect(req.headers.get("content-length")).toBeNull();
    return { req, state };
  }

  it("stops reading a chunked body once it passes the limit, and answers 413", async () => {
    const fetch = stubFetch(async () => Response.json(authBody));
    const { req, state } = streamedLogin(200);
    const res = await login(req);
    expect(res.status).toBe(413);
    expect(await res.json()).toMatchObject({ code: "payload_too_large" });
    expect(fetch).not.toHaveBeenCalled();
    // The limit is 4 chunks; reading must stop within a few chunks of it, not consume all 200.
    expect(state.pulled).toBeLessThan(10);
    expect(state.cancelled).toBe(true);
  });

  it("forwards a body of exactly the limit", async () => {
    const fetch = stubFetch(async () => Response.json(authBody));
    const { req } = streamedLogin(4);
    const res = await login(req);
    expect(res.status).toBe(200);
    expect((fetch.mock.calls[0]![1].body as ArrayBuffer).byteLength).toBe(64 * 1024);
  });
});
