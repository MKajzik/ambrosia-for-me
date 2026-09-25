import "server-only";

/** An RFC 9457 problem response in the same shape the Go API emits. */
export function problem(status: number, code: string, title: string, headers?: HeadersInit): Response {
  return new Response(JSON.stringify({ type: "about:blank", title, status, code }), {
    status,
    headers: { "Content-Type": "application/problem+json", "Cache-Control": "no-store", ...headers },
  });
}
