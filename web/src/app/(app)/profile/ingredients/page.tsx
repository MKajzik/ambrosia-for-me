import type { Metadata } from "next";
import { ArrowLeft } from "lucide-react";
import Link from "next/link";
import { PageHeader } from "@/components/page-header";
import { CustomIngredientsPage } from "@/features/ingredients/custom-ingredients-page";

export const metadata: Metadata = { title: "Custom ingredients" };

export default function Page() {
  return (
    <>
      <Link href="/profile" className="text-muted-foreground hover:text-foreground mb-4 inline-flex items-center gap-1 text-sm">
        <ArrowLeft aria-hidden className="size-4" />
        Profile
      </Link>
      <PageHeader title="Custom ingredients" description="The ingredients you made yourself, with their nutrition per 100 g." />
      <CustomIngredientsPage />
    </>
  );
}
