import "server-only";
import { apiBaseUrl } from "./env";

export type UpstreamInit = {
  method?: string;
  headers?: Headers;
  body?: BodyInit | null;
  /** Access token to send as `Authorization: Bearer`. */
  bearer?: string | null;
  /** Client address for the API's per-IP limits, sent as the only `X-Forwarded-For` entry. */
  clientIp?: string | null;
  signal?: AbortSignal;
};

/**
 * Calls the Go API. `path` starts with `/` and is relative to API_BASE_URL. The
 * browser's cookie, authorization and forwarding headers are never passed on:
 * identity is whatever this function is given, nothing more. Rejects (a
 * TypeError) when the API cannot be reached.
 */
export async function callApi(path: string, init: UpstreamInit = {}): Promise<Response> {
  const headers = new Headers(init.headers);
  for (const name of ["cookie", "authorization", "x-forwarded-for", "host", "content-length"]) headers.delete(name);
  if (init.bearer) headers.set("Authorization", `Bearer ${init.bearer}`);
  if (init.clientIp) headers.set("X-Forwarded-For", init.clientIp);
  return fetch(apiBaseUrl() + path, {
    method: init.method ?? "GET",
    headers,
    body: init.body ?? null,
    signal: init.signal,
    redirect: "manual",
    cache: "no-store",
  });
}
