import type { Metadata } from "next";
import { PageHeader } from "@/components/page-header";
import { PlanView } from "@/features/plan/plan-view";

export const metadata: Metadata = { title: "Plan" };

export default function Page() {
  return (
    <>
      <PageHeader title="Plan" />
      <PlanView />
    </>
  );
}
