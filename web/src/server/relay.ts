import "server-only";
import { NextResponse } from "next/server";
import { clearSessionCookies, setSessionCookies, type Tokens } from "./cookies";

/** The API response headers a browser may see; everything else (set-cookie, www-authenticate, ...) stays server-side. */
const RESPONSE_HEADERS = [
  "content-type",
  "cache-control",
  "etag",
  "retry-after",
  "allow",
  "x-request-id",
  "x-ratelimit-limit",
  "x-ratelimit-remaining",
  "x-ratelimit-reset",
];
/**
 * Turns an API response into the response for the browser: same status and body
 * (streamed, never buffered), the allowlisted headers, and optionally new or
 * cleared session cookies.
 */
export function relay(up: Response, opts: { issued?: Tokens | null; clear?: boolean } = {}): NextResponse {
  const headers = new Headers();
  for (const name of RESPONSE_HEADERS) {
    const value = up.headers.get(name);
    if (value) headers.set(name, value);
  }
  if (up.headers.get("content-type")?.toLowerCase().startsWith("text/event-stream")) {
    headers.set("Cache-Control", "no-cache, no-transform");
    headers.set("X-Accel-Buffering", "no");
  }
  const res = new NextResponse(up.body, { status: up.status, headers });
  if (opts.issued) setSessionCookies(res.cookies, opts.issued);
  if (opts.clear) clearSessionCookies(res.cookies);
  return res;
}
