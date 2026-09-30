import type { Metadata } from "next";
import { ShoppingListView } from "@/features/shopping/shopping-list-view";

export const metadata: Metadata = { title: "Shopping list" };

export default async function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return <ShoppingListView id={id} />;
}
