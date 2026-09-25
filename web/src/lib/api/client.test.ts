import { describe, expect, it, vi } from "vitest";
import { ApiError, createApi, unwrap } from "./client";

const user = { id: "u1", email: "a@b.test", display_name: "Ann", created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z" };

function fakeFetch(res: () => Response) {
  return vi.fn(async (_req: Request) => res());
}

describe("createApi", () => {
  it("calls the API through the base URL", async () => {
    const fetch = fakeFetch(() => Response.json(user));
    const api = createApi({ baseUrl: "http://app.test/api", fetch });
    expect(await unwrap(api.GET("/me"))).toEqual(user);
    expect(fetch.mock.calls[0]![0].url).toBe("http://app.test/api/me");
  });

  it("throws an ApiError carrying the code for a failed call", async () => {
    const api = createApi({
      baseUrl: "http://app.test/api",
      fetch: fakeFetch(() => Response.json({ code: "partner_not_linked" }, { status: 404, headers: { "Content-Type": "application/problem+json" } })),
    });
    await expect(unwrap(api.GET("/partner"))).rejects.toMatchObject({ name: "ApiError", status: 404, code: "partner_not_linked" });
    await expect(unwrap(api.GET("/partner"))).rejects.toBeInstanceOf(ApiError);
  });

  it("reports a 401 once per call and still throws", async () => {
    const onUnauthorized = vi.fn();
    const api = createApi({
      baseUrl: "http://app.test/api",
      fetch: fakeFetch(() => Response.json({ code: "unauthorized" }, { status: 401 })),
      onUnauthorized,
    });
    await expect(unwrap(api.GET("/me"))).rejects.toMatchObject({ status: 401 });
    expect(onUnauthorized).toHaveBeenCalledTimes(1);
  });

  it("does not report other failures as a lost session", async () => {
    const onUnauthorized = vi.fn();
    const api = createApi({ baseUrl: "http://app.test/api", fetch: fakeFetch(() => Response.json({ code: "internal_error" }, { status: 500 })), onUnauthorized });
    await expect(unwrap(api.GET("/me"))).rejects.toMatchObject({ status: 500 });
    expect(onUnauthorized).not.toHaveBeenCalled();
  });

  it("treats 204 as success with no data", async () => {
    const api = createApi({ baseUrl: "http://app.test/api", fetch: fakeFetch(() => new Response(null, { status: 204 })) });
    await expect(unwrap(api.DELETE("/me"))).resolves.toBeUndefined();
  });
});
