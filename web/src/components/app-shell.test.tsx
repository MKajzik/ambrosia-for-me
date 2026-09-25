// @vitest-environment jsdom
import { render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { AppShell } from "./app-shell";

let pathname = "/meals/abc";
vi.mock("next/navigation", () => ({ usePathname: () => pathname }));

describe("AppShell", () => {
  it("offers the five areas in both navigations and marks the current one", () => {
    pathname = "/meals/abc";
    render(<AppShell>content</AppShell>);
    const navs = screen.getAllByRole("navigation", { name: "Main" });
    expect(navs).toHaveLength(2);
    for (const nav of navs) {
      expect(within(nav).getAllByRole("link").map((a) => a.textContent)).toEqual(["Today", "Plan", "Meals", "Shopping", "Profile"]);
      expect(within(nav).getByRole("link", { name: "Meals" })).toHaveAttribute("aria-current", "page");
      expect(within(nav).getByRole("link", { name: "Plan" })).not.toHaveAttribute("aria-current");
    }
  });

  it("renders the page in a main landmark and offers a skip link to it", () => {
    render(<AppShell>page body</AppShell>);
    expect(screen.getByRole("main")).toHaveTextContent("page body");
    expect(screen.getByRole("link", { name: "Skip to content" })).toHaveAttribute("href", "#content");
  });

  it("gives every link a visible focus ring", () => {
    render(<AppShell>content</AppShell>);
    const links = [
      screen.getByRole("link", { name: "Skip to content" }),
      ...screen.getAllByRole("navigation", { name: "Main" }).flatMap((nav) => within(nav).getAllByRole("link")),
    ];
    expect(links).toHaveLength(11);
    for (const link of links) {
      expect(link.className).toContain("focus-visible:ring-");
    }
  });
});
