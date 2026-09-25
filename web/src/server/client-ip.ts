import { isIP } from "node:net";

/**
 * The client address as this server's own trusted proxies reported it: the Nth
 * entry of `X-Forwarded-For` counted from the right, where N is the number of
 * trusted proxies. Entries further left were supplied by the client or an
 * untrusted hop, so they are never used. Returns null when N is 0, the header
 * is missing or short, or the entry is not an IP address.
 */
export function forwardedClientIp(header: string | null, trusted: number): string | null {
  if (trusted <= 0 || !header) return null;
  const entries = header.split(",").map((e) => e.trim());
  if (entries.length < trusted) return null;
  const candidate = entries[entries.length - trusted];
  return candidate && isIP(candidate) !== 0 ? candidate : null;
}
