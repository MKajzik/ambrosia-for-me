import { describe, expect, it } from "vitest";
import { parseAuthResponse } from "./auth-response";

const good = {
  access_token: "a1",
  refresh_token: "r1",
  token_type: "Bearer",
  expires_in: 900,
  user: { id: "u1", email: "a@b.test" },
};

const json = (body: unknown) => Response.json(body);

describe("parseAuthResponse", () => {
  it("returns the tokens and the user from a well-formed response", async () => {
    expect(await parseAuthResponse(json(good))).toEqual({
      tokens: { accessToken: "a1", refreshToken: "r1", expiresIn: 900 },
      user: good.user,
    });
  });

  it("returns null for a body that is not JSON", async () => {
    expect(await parseAuthResponse(new Response("<html>Bad gateway</html>", { status: 200 }))).toBeNull();
  });

  it("returns null for an empty body", async () => {
    expect(await parseAuthResponse(new Response(null, { status: 204 }))).toBeNull();
  });

  it.each([null, "text", 5, []])("returns null when the JSON is %j rather than an object", async (body) => {
    expect(await parseAuthResponse(json(body))).toBeNull();
  });

  it.each(["access_token", "refresh_token", "expires_in", "user"])("returns null when %s is missing", async (field) => {
    const body: Record<string, unknown> = { ...good };
    delete body[field];
    expect(await parseAuthResponse(json(body))).toBeNull();
  });

  it("returns null when user is null", async () => {
    expect(await parseAuthResponse(json({ ...good, user: null }))).toBeNull();
  });

  it.each([
    ["access_token", 1],
    ["access_token", ""],
    ["refresh_token", 1],
    ["refresh_token", null],
    ["refresh_token", ""],
    ["expires_in", "900"],
    ["expires_in", 0],
    ["expires_in", -1],
    ["expires_in", null],
  ])("returns null when %s is %j", async (field, value) => {
    expect(await parseAuthResponse(json({ ...good, [field]: value }))).toBeNull();
  });

  it("returns null when expires_in is not finite", async () => {
    // JSON cannot carry Infinity, so hand the parser a response whose json() yields one.
    const res = { json: async () => ({ ...good, expires_in: Infinity }) } as unknown as Response;
    expect(await parseAuthResponse(res)).toBeNull();
  });
});
