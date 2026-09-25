import "server-only";
import { NextResponse, type NextRequest } from "next/server";
import { parseAuthResponse } from "./auth-response";
import { MAX_BODY_BYTES, readBounded } from "./body";
import { forwardedClientIp } from "./client-ip";
import { REFRESH_COOKIE, clearSessionCookies, setSessionCookies } from "./cookies";
import { trustedProxyCount } from "./env";
import { originGuard } from "./origin";
import { problem } from "./problem";
import { relay } from "./relay";
import { callApi } from "./upstream";

/**
 * Signs in or registers through the API. On success the tokens become httpOnly
 * cookies and the browser only gets `{ user }`; on failure the API's problem
 * response is relayed as is (validation errors, bad credentials, rate limits).
 */
async function authenticate(req: NextRequest, path: "/auth/login" | "/auth/register"): Promise<Response> {
  const refused = originGuard(req);
  if (refused) return refused;

  const body = await readBounded(req, MAX_BODY_BYTES);
  if (!body) return problem(413, "payload_too_large", "Request body too large");

  let up: Response;
  try {
    up = await callApi(path, {
      method: "POST",
      headers: new Headers({ "Content-Type": req.headers.get("content-type") ?? "application/json" }),
      body,
      clientIp: forwardedClientIp(req.headers.get("x-forwarded-for"), trustedProxyCount()),
      signal: req.signal,
    });
  } catch {
    return problem(502, "upstream_unavailable", "The API is unavailable");
  }
  if (!up.ok) return relay(up);

  const auth = await parseAuthResponse(up);
  if (!auth) return problem(502, "upstream_unavailable", "The API returned an unusable response");
  const res = NextResponse.json({ user: auth.user }, { status: up.status, headers: { "Cache-Control": "no-store" } });
  setSessionCookies(res.cookies, auth.tokens);
  return res;
}

export const login = (req: NextRequest) => authenticate(req, "/auth/login");
export const register = (req: NextRequest) => authenticate(req, "/auth/register");

/** Revokes the session at the API when there is one (best effort) and always clears the cookies. */
export async function logout(req: NextRequest): Promise<Response> {
  const refused = originGuard(req);
  if (refused) return refused;

  const refreshToken = req.cookies.get(REFRESH_COOKIE)?.value;
  if (refreshToken) {
    try {
      const up = await callApi("/auth/logout", {
        method: "POST",
        headers: new Headers({ "Content-Type": "application/json" }),
        body: JSON.stringify({ refresh_token: refreshToken }),
        clientIp: forwardedClientIp(req.headers.get("x-forwarded-for"), trustedProxyCount()),
      });
      await up.body?.cancel();
    } catch {
      // The session dies with the cookies either way; the token expires on its own.
    }
  }
  const res = new NextResponse(null, { status: 204, headers: { "Cache-Control": "no-store" } });
  clearSessionCookies(res.cookies);
  return res;
}
