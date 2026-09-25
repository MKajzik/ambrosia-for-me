import { describe, expect, it, vi } from "vitest";
import { RefreshUnavailableError, createRefresher } from "./refresh";

function pair(n: number) {
  return Response.json({ access_token: `a${n}`, refresh_token: `r${n}`, token_type: "Bearer", expires_in: 900 });
}
const tokens = (n: number) => ({ accessToken: `a${n}`, refreshToken: `r${n}`, expiresIn: 900 });

describe("createRefresher", () => {
  it("makes one API call for concurrent callers holding the same token", async () => {
    const call = vi.fn(async () => pair(1));
    const refresh = createRefresher({ call });
    const results = await Promise.all([refresh("old"), refresh("old"), refresh("old")]);
    expect(call).toHaveBeenCalledTimes(1);
    expect(results).toEqual([tokens(1), tokens(1), tokens(1)]);
  });

  it("hands a late caller with the already-rotated token the same new pair", async () => {
    let t = 1000;
    const call = vi.fn(async () => pair(1));
    const refresh = createRefresher({ call, now: () => t, ttlMs: 30_000 });
    await refresh("old");
    t += 29_999;
    expect(await refresh("old")).toEqual(tokens(1));
    expect(call).toHaveBeenCalledTimes(1);
  });

  it("asks the API again once the window has passed", async () => {
    let t = 1000;
    const call = vi.fn(async () => pair(1));
    const refresh = createRefresher({ call, now: () => t, ttlMs: 30_000 });
    await refresh("old");
    t += 30_000;
    await refresh("old");
    expect(call).toHaveBeenCalledTimes(2);
  });

  it("keeps different tokens apart", async () => {
    const call = vi.fn(async (token: string) => pair(token === "x" ? 1 : 2));
    const refresh = createRefresher({ call });
    expect(await refresh("x")).toEqual(tokens(1));
    expect(await refresh("y")).toEqual(tokens(2));
    expect(call).toHaveBeenCalledTimes(2);
  });

  it("resolves null when the API rejects the token, and does not remember that", async () => {
    const call = vi.fn(async () => Response.json({ code: "invalid_refresh_token" }, { status: 401 }));
    const refresh = createRefresher({ call });
    expect(await Promise.all([refresh("bad"), refresh("bad")])).toEqual([null, null]);
    expect(call).toHaveBeenCalledTimes(1);
    expect(await refresh("bad")).toBeNull();
    expect(call).toHaveBeenCalledTimes(2);
  });

  it("rejects, rather than ending the session, when the API is down or throttling", async () => {
    const down = createRefresher({ call: async () => new Response("", { status: 503 }) });
    await expect(down("t")).rejects.toBeInstanceOf(RefreshUnavailableError);
    const limited = createRefresher({ call: async () => new Response("", { status: 429 }) });
    await expect(limited("t")).rejects.toBeInstanceOf(RefreshUnavailableError);
    const unreachable = createRefresher({ call: async () => Promise.reject(new TypeError("fetch failed")) });
    await expect(unreachable("t")).rejects.toBeInstanceOf(RefreshUnavailableError);
  });

  it("retries after an unavailable answer instead of caching the failure", async () => {
    const call = vi.fn().mockResolvedValueOnce(new Response("", { status: 503 })).mockResolvedValueOnce(pair(1));
    const refresh = createRefresher({ call });
    await expect(refresh("t")).rejects.toBeInstanceOf(RefreshUnavailableError);
    expect(await refresh("t")).toEqual(tokens(1));
  });
});
