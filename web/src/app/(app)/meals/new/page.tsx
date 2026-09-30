import type { Metadata } from "next";
import { PageHeader } from "@/components/page-header";
import { BackLink } from "@/features/meals/back-link";
import { NewMealForm } from "@/features/meals/new-meal-form";

export const metadata: Metadata = { title: "New meal" };

export default function Page() {
  return (
    <>
      <BackLink />
      <PageHeader title="New meal" description="Name it and say how many servings it makes. You add ingredients next." />
      <NewMealForm />
    </>
  );
}
