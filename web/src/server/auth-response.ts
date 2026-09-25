import "server-only";
import type { Tokens } from "./cookies";

/**
 * Reads the API's `AuthResponse` (from login, register or refresh). Returns null
 * unless it is JSON with usable tokens and a user, so a proxy error page or a
 * truncated answer never turns into junk cookies.
 */
export async function parseAuthResponse(res: Response): Promise<{ tokens: Tokens; user: unknown } | null> {
  let body: unknown;
  try {
    body = await res.json();
  } catch {
    return null;
  }
  if (typeof body !== "object" || body === null) return null;
  const { access_token, refresh_token, expires_in, user } = body as Record<string, unknown>;
  if (typeof access_token !== "string" || !access_token) return null;
  if (typeof refresh_token !== "string" || !refresh_token) return null;
  if (typeof expires_in !== "number" || !Number.isFinite(expires_in) || expires_in <= 0) return null;
  if (user === undefined || user === null) return null;
  return { tokens: { accessToken: access_token, refreshToken: refresh_token, expiresIn: expires_in }, user };
}
