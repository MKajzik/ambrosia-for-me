import type { Metadata } from "next";
import { MealDetail } from "@/features/meals/meal-detail";

export const metadata: Metadata = { title: "Meal" };

export default async function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return <MealDetail id={id} />;
}
