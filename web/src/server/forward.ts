import "server-only";
import { NextResponse, type NextRequest } from "next/server";
import { ACCESS_COOKIE, REFRESH_COOKIE, clearSessionCookies, type Tokens } from "./cookies";
import { forwardedClientIp } from "./client-ip";
import { trustedProxyCount } from "./env";
import { originGuard } from "./origin";
import { problem } from "./problem";
import { refreshSession, type Refresher } from "./refresh";
import { relay } from "./relay";
import { callApi } from "./upstream";

/** The API rejects larger bodies itself; refusing here spares buffering them. */
const MAX_BODY_BYTES = 64 * 1024;

const REQUEST_HEADERS = ["accept", "accept-language", "content-type", "if-match", "if-none-match", "last-event-id"];


/** A path segment that must never reach the API: it could climb out of `/v1` or hit another route. */
function unsafeSegment(segment: string): boolean {
  return segment === "" || segment === "." || segment === ".." || /[/\\\0]/.test(segment);
}

function sessionOver(): NextResponse {
  const res = new NextResponse(problem(401, "unauthorized", "Your session has ended").body, {
    status: 401,
    headers: { "Content-Type": "application/problem+json", "Cache-Control": "no-store" },
  });
  clearSessionCookies(res.cookies);
  return res;
}

/**
 * Builds the handler behind `/api/[...path]`: it authenticates a browser request
 * from its cookies, calls the Go API with a bearer token, and streams the answer
 * back. An expired access token is refreshed (once, shared with concurrent
 * requests) and the call retried a single time.
 */
export function createForwarder({ refresh }: { refresh: Refresher }) {
  return async function forward(req: NextRequest, segments: string[]): Promise<Response> {
    const refused = originGuard(req);
    if (refused) return refused;

    if (segments.length === 0 || segments.some(unsafeSegment) || segments[0] === "auth") {
      return problem(404, "not_found", "Not found");
    }
    const path = "/" + segments.map(encodeURIComponent).join("/") + req.nextUrl.search;

    let body: ArrayBuffer | null = null;
    if (req.method !== "GET" && req.method !== "HEAD") {
      const declared = Number(req.headers.get("content-length") ?? 0);
      if (declared > MAX_BODY_BYTES) return problem(413, "payload_too_large", "Request body too large");
      body = await req.arrayBuffer();
      if (body.byteLength > MAX_BODY_BYTES) return problem(413, "payload_too_large", "Request body too large");
    }

    const headers = new Headers();
    for (const name of REQUEST_HEADERS) {
      const value = req.headers.get(name);
      if (value) headers.set(name, value);
    }
    const clientIp = forwardedClientIp(req.headers.get("x-forwarded-for"), trustedProxyCount());
    const send = (bearer: string | null) =>
      callApi(path, { method: req.method, headers, body, bearer, clientIp, signal: req.signal });

    const refreshToken = req.cookies.get(REFRESH_COOKIE)?.value;
    let bearer = req.cookies.get(ACCESS_COOKIE)?.value ?? null;
    let issued: Tokens | null = null;

    try {
      if (!bearer && refreshToken) {
        issued = await refresh(refreshToken);
        if (!issued) return sessionOver();
        bearer = issued.accessToken;
      }

      let up = await send(bearer);

      if (up.status === 401 && refreshToken && !issued) {
        await up.body?.cancel();
        issued = await refresh(refreshToken);
        if (!issued) return sessionOver();
        up = await send(issued.accessToken);
        if (up.status === 401) {
          await up.body?.cancel();
          return sessionOver();
        }
      }
      return relay(up, { issued });
    } catch {
      // The API (or the refresh call) could not be reached; the session is not over.
      return problem(502, "upstream_unavailable", "The API is unavailable");
    }
  };
}

export const forward = createForwarder({ refresh: refreshSession });
