import { NextResponse, type NextRequest } from "next/server";
import { REFRESH_COOKIE } from "@/server/cookies";

const AUTH_PAGES = new Set(["/login", "/register"]);

/**
 * Page guard. A browser with no refresh cookie is sent to /login (remembering
 * where it was going); one that has a session is kept off the login and
 * register pages. This only looks at the cookie's presence: whether the
 * session is still good is settled by the first API call, which clears the
 * cookies when it is not.
 */
export function proxy(req: NextRequest) {
  const { pathname, search } = req.nextUrl;
  const signedIn = req.cookies.has(REFRESH_COOKIE);
  const authPage = AUTH_PAGES.has(pathname);

  if (!signedIn && !authPage) {
    const to = req.nextUrl.clone();
    to.pathname = "/login";
    to.search = "";
    if (pathname !== "/") to.searchParams.set("next", pathname + search);
    return NextResponse.redirect(to);
  }
  if (signedIn && authPage) {
    const to = req.nextUrl.clone();
    to.pathname = "/today";
    to.search = "";
    return NextResponse.redirect(to);
  }
  return NextResponse.next();
}

export const config = {
  // Everything except the API (which answers 401 itself), Next internals and files with an extension.
  matcher: ["/((?!api/|_next/|.*\\..*).*)"],
};
