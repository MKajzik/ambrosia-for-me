import type { Metadata } from "next";
import { ComingSoon } from "@/components/coming-soon";
import { PageHeader } from "@/components/page-header";

export const metadata: Metadata = { title: "Plan" };

export default function Page() {
  return (
    <>
      <PageHeader title="Plan" />
      <ComingSoon>The week calendar and diet templates land with the Plan and Today screens.</ComingSoon>
    </>
  );
}
