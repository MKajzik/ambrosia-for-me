import { NextRequest } from "next/server";
import { describe, expect, it } from "vitest";
import { proxy } from "./proxy";

function page(path: string, cookie?: string) {
  return new NextRequest(`http://app.test${path}`, { headers: cookie ? { cookie } : {} });
}
const location = (res: Response) => (res.headers.get("location") ? new URL(res.headers.get("location")!) : null);

describe("page guard", () => {
  it("sends a visitor without a session to /login and remembers the page", () => {
    const res = proxy(page("/meals?tab=partner"));
    expect(res.status).toBe(307);
    const to = location(res)!;
    expect(to.pathname).toBe("/login");
    expect(to.searchParams.get("next")).toBe("/meals?tab=partner");
  });

  it("does not add a next parameter for the site root", () => {
    expect(location(proxy(page("/")))!.search).toBe("");
  });

  it("lets a visitor without a session reach /login and /register", () => {
    expect(proxy(page("/login")).headers.get("location")).toBeNull();
    expect(proxy(page("/register")).headers.get("location")).toBeNull();
  });

  it("lets a signed-in visitor through, and keeps them off the login pages", () => {
    const cookie = "mp_refresh=r1";
    expect(proxy(page("/today", cookie)).headers.get("location")).toBeNull();
    expect(location(proxy(page("/login", cookie)))!.pathname).toBe("/today");
    expect(location(proxy(page("/register", cookie)))!.pathname).toBe("/today");
  });

  it("treats only the refresh cookie as a session", () => {
    expect(location(proxy(page("/today", "mp_access=a1")))!.pathname).toBe("/login");
  });
});
