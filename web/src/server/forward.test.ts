import { NextRequest } from "next/server";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createForwarder } from "./forward";
import { RefreshUnavailableError } from "./refresh";

const newTokens = { accessToken: "a-new", refreshToken: "r-new", expiresIn: 900 };
type Fetch = (url: string, init: RequestInit & { headers: Headers }) => Promise<Response>;

function stubFetch(impl: Fetch) {
  const fn = vi.fn(impl);
  vi.stubGlobal("fetch", fn);
  return fn;
}

function request(path: string, init: { method?: string; headers?: Record<string, string>; cookies?: Record<string, string>; body?: string; signal?: AbortSignal } = {}) {
  const headers: Record<string, string> = { host: "app.test", ...init.headers };
  if (init.method && init.method !== "GET") {
    headers.origin ??= "http://app.test";
    if (init.body) headers["content-type"] ??= "application/json";
  }
  if (init.cookies) headers.cookie = Object.entries(init.cookies).map(([k, v]) => `${k}=${v}`).join("; ");
  return new NextRequest(`http://app.test/api${path}`, { method: init.method, headers, body: init.body, signal: init.signal });
}

const setCookies = (res: Response) => res.headers.getSetCookie();
const ok = () => Response.json({ ok: true });

beforeEach(() => vi.stubEnv("API_BASE_URL", "http://api.test/v1"));
afterEach(() => vi.unstubAllEnvs());

describe("forward", () => {
  it("maps the path and query onto the API and sends only the bearer token, not the browser's cookies", async () => {
    const fetch = stubFetch(async () => ok());
    const forward = createForwarder({ refresh: vi.fn() });
    const res = await forward(
      request("/meals?limit=5", { cookies: { mp_access: "acc", mp_refresh: "ref" }, headers: { authorization: "Bearer forged", accept: "application/json" } }),
      ["meals"],
    );
    expect(res.status).toBe(200);
    const [url, init] = fetch.mock.calls[0]!;
    expect(url).toBe("http://api.test/v1/meals?limit=5");
    expect(init.headers.get("authorization")).toBe("Bearer acc");
    expect(init.headers.get("cookie")).toBeNull();
    expect(init.headers.get("accept")).toBe("application/json");
    expect(setCookies(res)).toEqual([]);
  });

  it("refreshes first when only the refresh cookie is present, and sets the new cookies", async () => {
    const fetch = stubFetch(async () => ok());
    const refresh = vi.fn(async () => newTokens);
    const res = await createForwarder({ refresh })(request("/me", { cookies: { mp_refresh: "r-old" } }), ["me"]);
    expect(refresh).toHaveBeenCalledWith("r-old");
    expect(fetch.mock.calls[0]![1].headers.get("authorization")).toBe("Bearer a-new");
    const cookies = setCookies(res).join("\n");
    expect(cookies).toMatch(/mp_access=a-new;.*Path=\/api/);
    expect(cookies).toMatch(/mp_refresh=r-new;.*Path=\//);
  });

  it("refreshes and retries once, with the same body, when the API answers 401", async () => {
    const fetch = stubFetch(async (_url, init) =>
      init.headers.get("authorization") === "Bearer a-new" ? ok() : Response.json({ code: "unauthorized" }, { status: 401 }),
    );
    const refresh = vi.fn(async () => newTokens);
    const res = await createForwarder({ refresh })(
      request("/meals", { method: "POST", body: '{"name":"Soup"}', cookies: { mp_access: "stale", mp_refresh: "r-old" } }),
      ["meals"],
    );
    expect(res.status).toBe(200);
    expect(fetch).toHaveBeenCalledTimes(2);
    const sent = fetch.mock.calls.map(([, init]) => new TextDecoder().decode(init.body as ArrayBuffer));
    expect(sent).toEqual(['{"name":"Soup"}', '{"name":"Soup"}']);
    expect(setCookies(res).join("\n")).toContain("mp_refresh=r-new");
  });

  it("ends the session (401 and cleared cookies) when the refresh token is rejected, without calling the API", async () => {
    const fetch = stubFetch(async () => ok());
    const res = await createForwarder({ refresh: async () => null })(request("/me", { cookies: { mp_refresh: "dead" } }), ["me"]);
    expect(res.status).toBe(401);
    expect(await res.json()).toMatchObject({ code: "unauthorized" });
    expect(fetch).not.toHaveBeenCalled();
    expect(setCookies(res).join("\n")).toMatch(/mp_access=;.*Max-Age=0/);
    expect(setCookies(res).join("\n")).toMatch(/mp_refresh=;.*Max-Age=0/);
  });

  it("ends the session when a freshly refreshed token is still refused", async () => {
    stubFetch(async () => Response.json({ code: "unauthorized" }, { status: 401 }));
    const res = await createForwarder({ refresh: async () => newTokens })(request("/me", { cookies: { mp_access: "x", mp_refresh: "r" } }), ["me"]);
    expect(res.status).toBe(401);
    expect(setCookies(res).join("\n")).toMatch(/mp_refresh=;.*Max-Age=0/);
  });

  it("keeps the session and answers 502 when the refresh is merely unavailable", async () => {
    const fetch = stubFetch(async () => ok());
    const refresh = vi.fn(async () => Promise.reject(new RefreshUnavailableError("down")));
    const res = await createForwarder({ refresh })(request("/me", { cookies: { mp_refresh: "r" } }), ["me"]);
    expect(res.status).toBe(502);
    expect(await res.json()).toMatchObject({ code: "upstream_unavailable" });
    expect(fetch).not.toHaveBeenCalled();
    expect(setCookies(res)).toEqual([]);
  });

  it("passes a 401 through untouched when there is no session at all", async () => {
    const fetch = stubFetch(async () => Response.json({ code: "unauthorized" }, { status: 401 }));
    const res = await createForwarder({ refresh: vi.fn() })(request("/me"), ["me"]);
    expect(res.status).toBe(401);
    expect(fetch.mock.calls[0]![1].headers.get("authorization")).toBeNull();
    expect(setCookies(res)).toEqual([]);
  });

  it("answers 502 when the API cannot be reached", async () => {
    stubFetch(async () => Promise.reject(new TypeError("fetch failed")));
    const res = await createForwarder({ refresh: vi.fn() })(request("/me", { cookies: { mp_access: "a" } }), ["me"]);
    expect(res.status).toBe(502);
  });

  it("passes the API's problem response through with its status and rate-limit headers", async () => {
    stubFetch(async () =>
      new Response(JSON.stringify({ code: "rate_limited" }), {
        status: 429,
        headers: { "Content-Type": "application/problem+json", "Retry-After": "12", "WWW-Authenticate": "Bearer", "Set-Cookie": "evil=1" },
      }),
    );
    const res = await createForwarder({ refresh: vi.fn() })(request("/meals", { cookies: { mp_access: "a" } }), ["meals"]);
    expect(res.status).toBe(429);
    expect(res.headers.get("retry-after")).toBe("12");
    expect(res.headers.get("content-type")).toBe("application/problem+json");
    expect(res.headers.get("www-authenticate")).toBeNull();
    expect(setCookies(res)).toEqual([]);
  });

  it.each([["auth", "refresh"], ["auth"], [".."], [".", "meals"], ["a/b"], ["meals", ""], ["..\\x"], []])("never forwards the path %j", async (...segments) => {
    const fetch = stubFetch(async () => ok());
    const res = await createForwarder({ refresh: vi.fn() })(request("/x", { cookies: { mp_access: "a" } }), segments as string[]);
    expect(res.status).toBe(404);
    expect(fetch).not.toHaveBeenCalled();
  });

  it("refuses a cross-origin write before doing anything else", async () => {
    const fetch = stubFetch(async () => ok());
    const refresh = vi.fn();
    const res = await createForwarder({ refresh })(
      request("/meals", { method: "POST", body: "{}", headers: { origin: "http://evil.test" }, cookies: { mp_refresh: "r" } }),
      ["meals"],
    );
    expect(res.status).toBe(403);
    expect(fetch).not.toHaveBeenCalled();
    expect(refresh).not.toHaveBeenCalled();
  });

  it("refuses a body larger than the API accepts", async () => {
    const fetch = stubFetch(async () => ok());
    const res = await createForwarder({ refresh: vi.fn() })(
      request("/meals", { method: "POST", body: "x".repeat(64 * 1024 + 1), cookies: { mp_access: "a" } }),
      ["meals"],
    );
    expect(res.status).toBe(413);
    expect(fetch).not.toHaveBeenCalled();
  });

  it("streams an event stream through without buffering it", async () => {
    let push!: (chunk: string) => void;
    const upstream = new ReadableStream<Uint8Array>({
      start(controller) {
        const enc = new TextEncoder();
        controller.enqueue(enc.encode("event: item_changed\ndata: {}\n\n"));
        push = (chunk) => controller.enqueue(enc.encode(chunk));
      },
    });
    stubFetch(async () => new Response(upstream, { headers: { "Content-Type": "text/event-stream" } }));
    const res = await createForwarder({ refresh: vi.fn() })(request("/shopping-lists/1/events", { cookies: { mp_access: "a" } }), ["shopping-lists", "1", "events"]);
    expect(res.headers.get("content-type")).toBe("text/event-stream");
    expect(res.headers.get("cache-control")).toBe("no-cache, no-transform");
    const reader = res.body!.getReader();
    // The first chunk arrives while the upstream is still open and has sent nothing more.
    expect(new TextDecoder().decode((await reader.read()).value)).toBe("event: item_changed\ndata: {}\n\n");
    push(": keep-alive\n\n");
    expect(new TextDecoder().decode((await reader.read()).value)).toBe(": keep-alive\n\n");
    await reader.cancel();
  });

  it("cancels the API call when the browser goes away", async () => {
    let seen: AbortSignal | null | undefined;
    stubFetch(async (_url, init) => {
      seen = init.signal;
      return ok();
    });
    const ac = new AbortController();
    await createForwarder({ refresh: vi.fn() })(request("/x", { cookies: { mp_access: "a" }, signal: ac.signal }), ["x"]);
    expect(seen?.aborted).toBe(false);
    ac.abort();
    expect(seen?.aborted).toBe(true);
  });

  describe("client address", () => {
    it("is never taken from X-Forwarded-For when no proxy is trusted, even if the client sets it", async () => {
      const fetch = stubFetch(async () => ok());
      await createForwarder({ refresh: vi.fn() })(request("/x", { cookies: { mp_access: "a" }, headers: { "x-forwarded-for": "6.6.6.6" } }), ["x"]);
      expect(fetch.mock.calls[0]![1].headers.get("x-forwarded-for")).toBeNull();
    });

    it("is the entry the trusted proxy appended, sent as the only X-Forwarded-For value", async () => {
      vi.stubEnv("WEB_TRUSTED_PROXY_COUNT", "1");
      const fetch = stubFetch(async () => ok());
      await createForwarder({ refresh: vi.fn() })(request("/x", { cookies: { mp_access: "a" }, headers: { "x-forwarded-for": "6.6.6.6, 7.7.7.7" } }), ["x"]);
      expect(fetch.mock.calls[0]![1].headers.get("x-forwarded-for")).toBe("7.7.7.7");
    });
  });
});
