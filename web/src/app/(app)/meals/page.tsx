import type { Metadata } from "next";
import { PageHeader } from "@/components/page-header";
import { MealsPage } from "@/features/meals/meals-page";

export const metadata: Metadata = { title: "Meals" };

export default function Page() {
  return (
    <>
      <PageHeader title="Meals" />
      <MealsPage />
    </>
  );
}
