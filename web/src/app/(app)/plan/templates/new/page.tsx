import type { Metadata } from "next";
import Link from "next/link";
import { ArrowLeft } from "lucide-react";
import { PageHeader } from "@/components/page-header";
import { NewTemplateForm } from "@/features/plan/new-template-form";

export const metadata: Metadata = { title: "New diet template" };

export default function Page() {
  return (
    <>
      <Link href="/plan/templates" className="text-muted-foreground hover:text-foreground mb-4 inline-flex items-center gap-1 text-sm">
        <ArrowLeft aria-hidden className="size-4" />
        Diet templates
      </Link>
      <PageHeader title="New diet template" description="Name it and choose how many days it covers. You add meals next." />
      <NewTemplateForm />
    </>
  );
}
