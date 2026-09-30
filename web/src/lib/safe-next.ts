const FALLBACK = "/today";

/**
 * The in-app path a `?next=` value points to, or `/today` when it is missing or
 * would leave this origin (`//host`, `/\host`, `https://host`, `javascript:`).
 * Dot segments (. and ..) normalize during URL parsing; a path like `/..//evil`
 * becomes `//evil` which is protocol-relative. We reject those and backslashes.
 */
export function safeNext(raw: string | null | undefined): string {
  if (!raw) return FALLBACK;
  try {
    const base = "http://internal.invalid";
    const url = new URL(raw, base);
    if (url.origin !== base || !raw.startsWith("/")) return FALLBACK;
    const out = url.pathname + url.search + url.hash;
    if (out.startsWith("//") || out.includes("\\")) return FALLBACK;
    return out;
  } catch {
    return FALLBACK;
  }
}
