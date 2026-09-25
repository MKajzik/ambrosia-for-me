import "server-only";
import { secureCookies } from "./env";

export const ACCESS_COOKIE = "mp_access";
export const REFRESH_COOKIE = "mp_refresh";

/** Matches the API's refresh-token lifetime (30 days); the API is what actually enforces it. */
const REFRESH_MAX_AGE_SECONDS = 30 * 24 * 60 * 60;

export type Tokens = { accessToken: string; refreshToken: string; expiresIn: number };

type CookieOptions = {
  httpOnly: boolean;
  secure: boolean;
  sameSite: "lax";
  path: string;
  maxAge: number;
};

/** The part of `NextResponse#cookies` this module needs. */
export interface CookieWriter {
  set(name: string, value: string, options: CookieOptions): unknown;
}

function options(path: string, maxAge: number): CookieOptions {
  return { httpOnly: true, secure: secureCookies(), sameSite: "lax", path, maxAge };
}

/**
 * The access token is only ever read by the API proxy, so it stays under /api.
 * The refresh token is scoped to / because the page guard (proxy.ts) needs to see it
 * on page requests; it is still httpOnly and only sent to this origin.
 */
export function setSessionCookies(jar: CookieWriter, tokens: Tokens): void {
  jar.set(ACCESS_COOKIE, tokens.accessToken, options("/api", tokens.expiresIn));
  jar.set(REFRESH_COOKIE, tokens.refreshToken, options("/", REFRESH_MAX_AGE_SECONDS));
}

export function clearSessionCookies(jar: CookieWriter): void {
  jar.set(ACCESS_COOKIE, "", options("/api", 0));
  jar.set(REFRESH_COOKIE, "", options("/", 0));
}
