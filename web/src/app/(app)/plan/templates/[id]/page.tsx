import type { Metadata } from "next";
import { TemplateDetail } from "@/features/plan/template-detail";

export const metadata: Metadata = { title: "Diet template" };

export default async function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return <TemplateDetail id={id} />;
}
