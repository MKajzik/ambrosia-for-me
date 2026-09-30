import type { Metadata } from "next";
import { PageHeader } from "@/components/page-header";
import { TemplatesPage } from "@/features/plan/templates-page";

export const metadata: Metadata = { title: "Diet templates" };

export default function Page() {
  return (
    <>
      <PageHeader title="Diet templates" />
      <TemplatesPage />
    </>
  );
}
