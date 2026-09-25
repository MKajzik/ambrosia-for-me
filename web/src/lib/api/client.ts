import createClient from "openapi-fetch";
import { hardNavigate } from "@/lib/navigation";
import { ApiError, toApiError } from "./problem";
import type { paths } from "./schema.gen";

type Options = {
  baseUrl: string;
  fetch?: (input: Request) => Promise<Response>;
  /** Called (once per call) when the API answers 401, i.e. the session is over. */
  onUnauthorized?: () => void;
};

/** A typed API client. Everything goes through the same-origin `/api` proxy, which owns the tokens. */
export function createApi({ baseUrl, fetch: fetchImpl, onUnauthorized }: Options) {
  // openapi-fetch would otherwise capture globalThis.fetch once, at creation; looking it up per call
  // keeps the client honest about whatever fetch is current (and stubbable in tests).
  const client = createClient<paths>({ baseUrl, fetch: fetchImpl ?? ((input) => globalThis.fetch(input)) });
  client.use({
    onResponse({ response }) {
      if (response.status === 401) onUnauthorized?.();
    },
  });
  return client;
}

/** Sends the browser to the sign-in page, remembering where it was, unless it is already on an auth page. */
export function redirectToLogin(): void {
  const { pathname, search } = window.location;
  if (pathname === "/login" || pathname === "/register") return;
  hardNavigate(`/login?next=${encodeURIComponent(pathname + search)}`);
}

// Request() wants an absolute URL outside a real browser page (jsdom, Node), so the same-origin base is resolved up front.
const apiBase = typeof window === "undefined" ? "/api" : `${window.location.origin}/api`;

export const api = createApi({ baseUrl: apiBase, onUnauthorized: redirectToLogin });

type Result<T> = { data?: T; error?: unknown; response: Response };

/** The data of a successful call; throws an ApiError for any other answer. */
export async function unwrap<T>(call: Promise<Result<T>>): Promise<T> {
  const { data, error, response } = await call;
  // openapi-fetch has already read the body: a failure's problem is in `error`.
  if (!response.ok) throw toApiError(response.status, error, response.headers);
  return data as T;
}

export { ApiError };
