/**
 * A full page load. Used when the session ends (sign-out, or the API says it is over): it drops
 * every in-memory cache with the dead session, and cannot race a query that is about to refetch.
 */
export function hardNavigate(url: string): void {
  window.location.assign(url);
}
