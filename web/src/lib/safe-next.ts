const FALLBACK = "/today";

/**
 * The in-app path a `?next=` value points to, or `/today` when it is missing or
 * would leave this origin (`//host`, `/\host`, `https://host`, `javascript:`).
 */
export function safeNext(raw: string | null | undefined): string {
  if (!raw) return FALLBACK;
  try {
    const base = "http://internal.invalid";
    const url = new URL(raw, base);
    if (url.origin !== base || !raw.startsWith("/")) return FALLBACK;
    return url.pathname + url.search + url.hash;
  } catch {
    return FALLBACK;
  }
}
