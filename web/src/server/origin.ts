import "server-only";
import { problem } from "./problem";

const SAFE_METHODS = new Set(["GET", "HEAD", "OPTIONS"]);

/**
 * CSRF guard for cookie-authenticated requests. A state-changing request must
 * carry an Origin header whose host is this server's own Host, and, when it
 * declares a content type at all, that type must be JSON (a cross-site form
 * cannot send it). Returns a 403 problem response when the request is refused.
 */
export function originGuard(req: Request): Response | null {
  if (SAFE_METHODS.has(req.method.toUpperCase())) return null;

  const origin = req.headers.get("origin");
  const host = req.headers.get("host");
  let originHost: string | null = null;
  try {
    originHost = origin ? new URL(origin).host : null;
  } catch {
    originHost = null;
  }
  if (!originHost || !host || originHost !== host) {
    return problem(403, "csrf_rejected", "Cross-origin request refused");
  }

  const contentType = req.headers.get("content-type");
  if (contentType && !contentType.toLowerCase().startsWith("application/json")) {
    return problem(403, "csrf_rejected", "Unsupported content type");
  }
  return null;
}
