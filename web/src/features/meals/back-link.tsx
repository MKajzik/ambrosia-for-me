import { ArrowLeft } from "lucide-react";
import Link from "next/link";

export function BackLink() {
  return (
    <Link href="/meals" className="text-muted-foreground hover:text-foreground mb-4 inline-flex items-center gap-1 text-sm">
      <ArrowLeft aria-hidden className="size-4" />
      Meals
    </Link>
  );
}
