import { vi } from "vitest";

export type FakeRequest = { method: string; path: string; params: Record<string, string>; search: URLSearchParams; body: unknown };
type Handler = (req: FakeRequest) => Response | Promise<Response>;

export function json(body: unknown, status = 200): Response {
  return Response.json(body, { status });
}

/** A problem+json answer, shaped like the API's (`code` is the stable identifier the UI keys on). */
export function problem(status: number, code: string, extra: Record<string, unknown> = {}): Response {
  return new Response(JSON.stringify({ type: `urn:mealplanner:problem:${code}`, title: "Problem", status, code, ...extra }), {
    status,
    headers: { "content-type": "application/problem+json" },
  });
}

export function noContent(): Response {
  return new Response(null, { status: 204 });
}

function compile(pattern: string) {
  const [method = "", path = ""] = pattern.split(" ");
  const names: string[] = [];
  const source = path.replace(/:([a-z_]+)/g, (_match, name: string) => {
    names.push(name);
    return "([^/]+)";
  });
  return { method, regex: new RegExp(`^${source}$`), names };
}

/**
 * Stubs `fetch` with a router keyed like `"GET /meals/:id"` (paths are relative to `/api`).
 * Requests nobody registered throw, so a test cannot pass by silently hitting nothing.
 */
export function fakeApi(routes: Record<string, Handler>) {
  const compiled = Object.entries(routes).map(([pattern, handler]) => ({ ...compile(pattern), handler }));
  const calls: FakeRequest[] = [];
  const stub = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const request = input instanceof Request ? input : new Request(new URL(String(input), "http://localhost:3000"), init);
    const url = new URL(request.url);
    const path = url.pathname.replace(/^\/api/, "");
    const text = await request.clone().text();
    const body = text ? (JSON.parse(text) as unknown) : null;
    for (const route of compiled) {
      const match = route.regex.exec(path);
      if (route.method !== request.method || !match) continue;
      const req: FakeRequest = {
        method: request.method,
        path,
        params: Object.fromEntries(route.names.map((name, i) => [name, decodeURIComponent(match[i + 1] ?? "")])),
        search: url.searchParams,
        body,
      };
      calls.push(req);
      return route.handler(req);
    }
    throw new Error(`fakeApi: no route for ${request.method} ${path}`);
  });
  vi.stubGlobal("fetch", stub);
  return {
    calls,
    callsTo: (method: string, path: string) => calls.filter((c) => c.method === method && c.path === path),
  };
}
