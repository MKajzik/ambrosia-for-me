import { CalendarDays, ShoppingBasket, Sun, UserRound, UtensilsCrossed, type LucideIcon } from "lucide-react";

export type NavItem = { href: string; label: string; icon: LucideIcon };

/** The five product areas, in the order both the sidebar and the tab bar show them. */
export const NAV_ITEMS: NavItem[] = [
  { href: "/today", label: "Today", icon: Sun },
  { href: "/plan", label: "Plan", icon: CalendarDays },
  { href: "/meals", label: "Meals", icon: UtensilsCrossed },
  { href: "/shopping", label: "Shopping", icon: ShoppingBasket },
  { href: "/profile", label: "Profile", icon: UserRound },
];

/** Whether `pathname` is inside the area `href` (so `/meals/abc` highlights Meals). */
export function isActive(pathname: string, href: string): boolean {
  return pathname === href || pathname.startsWith(`${href}/`);
}
