import type { Metadata } from "next";
import { ComingSoon } from "@/components/coming-soon";
import { PageHeader } from "@/components/page-header";

export const metadata: Metadata = { title: "Today" };

export default function Page() {
  return (
    <>
      <PageHeader title="Today" />
      <ComingSoon>Calories, macros and the meals for the day land with the Plan and Today screens.</ComingSoon>
    </>
  );
}
