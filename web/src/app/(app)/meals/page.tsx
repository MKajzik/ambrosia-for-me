import type { Metadata } from "next";
import { ComingSoon } from "@/components/coming-soon";
import { PageHeader } from "@/components/page-header";

export const metadata: Metadata = { title: "Meals" };

export default function Page() {
  return (
    <>
      <PageHeader title="Meals" />
      <ComingSoon>Your meal library and the meals shared with you land with the Meals screens.</ComingSoon>
    </>
  );
}
