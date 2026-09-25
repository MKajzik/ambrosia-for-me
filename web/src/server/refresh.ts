import "server-only";
import type { Tokens } from "./cookies";
import { callApi } from "./upstream";

/** The API could not be asked, or refused for a reason that is not a bad token. */
export class RefreshUnavailableError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "RefreshUnavailableError";
  }
}

export type Refresher = (refreshToken: string) => Promise<Tokens | null>;

type Deps = {
  /** Sends `POST /auth/refresh` with this refresh token. */
  call: (refreshToken: string) => Promise<Response>;
  now?: () => number;
  /** How long a completed rotation is remembered for late arrivals (default 30 s). */
  ttlMs?: number;
};

/**
 * Rotating a refresh token invalidates it, and presenting an already-used one
 * revokes the whole session family. A page fires several API calls at once, so
 * they must all see one rotation: callers presenting the same token share the
 * request in flight, and for `ttlMs` afterwards, the result. Resolves null when
 * the API says the token is invalid (the session is over), and rejects with
 * RefreshUnavailableError on anything else, which must not end the session.
 *
 * State is per process: run one web instance until a shared store exists.
 */
export function createRefresher({ call, now = Date.now, ttlMs = 30_000 }: Deps): Refresher {
  const inflight = new Map<string, Promise<Tokens | null>>();
  const recent = new Map<string, { tokens: Tokens; expiresAt: number }>();

  async function rotate(refreshToken: string): Promise<Tokens | null> {
    let res: Response;
    try {
      res = await call(refreshToken);
    } catch {
      throw new RefreshUnavailableError("api unreachable");
    }
    if (res.status === 400 || res.status === 401) return null;
    if (!res.ok) throw new RefreshUnavailableError(`refresh answered ${res.status}`);
    const body = (await res.json()) as { access_token: string; refresh_token: string; expires_in: number };
    return { accessToken: body.access_token, refreshToken: body.refresh_token, expiresIn: body.expires_in };
  }

  return async (refreshToken) => {
    const t = now();
    for (const [key, entry] of recent) if (entry.expiresAt <= t) recent.delete(key);

    const done = recent.get(refreshToken);
    if (done) return done.tokens;

    let pending = inflight.get(refreshToken);
    if (!pending) {
      pending = rotate(refreshToken)
        .then((tokens) => {
          if (tokens) recent.set(refreshToken, { tokens, expiresAt: now() + ttlMs });
          return tokens;
        })
        .finally(() => inflight.delete(refreshToken));
      inflight.set(refreshToken, pending);
    }
    return pending;
  };
}

/** The process-wide refresher used by the API proxy. */
export const refreshSession: Refresher = createRefresher({
  call: (refreshToken) =>
    callApi("/auth/refresh", {
      method: "POST",
      headers: new Headers({ "Content-Type": "application/json" }),
      body: JSON.stringify({ refresh_token: refreshToken }),
    }),
});
