import type { Metadata } from "next";
import { PageHeader } from "@/components/page-header";
import { ShoppingPage } from "@/features/shopping/shopping-page";

export const metadata: Metadata = { title: "Shopping" };

export default function Page() {
  return (
    <>
      <PageHeader title="Shopping" />
      <ShoppingPage />
    </>
  );
}
