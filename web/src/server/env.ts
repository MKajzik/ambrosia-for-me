import "server-only";

/** Base URL of the Go API including its `/v1` prefix, without a trailing slash. */
export function apiBaseUrl(): string {
  return (process.env.API_BASE_URL ?? "http://localhost:8080/v1").replace(/\/+$/, "");
}

/**
 * How many trusted reverse proxies sit in front of this server and append to
 * `X-Forwarded-For`. 0 (the default) means none: the header is then never read,
 * because any client can forge it.
 */
export function trustedProxyCount(): number {
  const raw = process.env.WEB_TRUSTED_PROXY_COUNT;
  if (!raw) return 0;
  const n = Number(raw);
  return Number.isInteger(n) && n >= 0 ? n : 0;
}

/** Session cookies are `Secure` unless COOKIE_SECURE=false (plain-http local stacks). */
export function secureCookies(): boolean {
  const flag = process.env.COOKIE_SECURE;
  if (flag === "true") return true;
  if (flag === "false") return false;
  return process.env.NODE_ENV === "production";
}
