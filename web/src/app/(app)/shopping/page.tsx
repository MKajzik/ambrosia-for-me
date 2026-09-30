import type { Metadata } from "next";
import { ComingSoon } from "@/components/coming-soon";
import { PageHeader } from "@/components/page-header";

export const metadata: Metadata = { title: "Shopping" };

export default function Page() {
  return (
    <>
      <PageHeader title="Shopping" />
      <ComingSoon>Shopping lists land with the Shopping and Profile screens.</ComingSoon>
    </>
  );
}
