import type { Metadata } from "next";
import { PageHeader } from "@/components/page-header";
import { TodayView } from "@/features/plan/today-view";

export const metadata: Metadata = { title: "Today" };

export default function Page() {
  return (
    <>
      <PageHeader title="Today" />
      <TodayView />
    </>
  );
}
