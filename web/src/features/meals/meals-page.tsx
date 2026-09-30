"use client";

import { Plus } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { MealList } from "./meal-list";
import { usePartnerLink } from "./queries";

/** My meals, and, while I have a partner, the meals they share. */
export function MealsPage() {
  const link = usePartnerLink();
  const [tab, setTab] = useState<"mine" | "partner">("mine");
  const partnerActive = link.data?.status === "active";

  return (
    <div className="grid gap-4">
      <div className="flex justify-end">
        <Button asChild>
          <Link href="/meals/new">
            <Plus aria-hidden />
            New meal
          </Link>
        </Button>
      </div>
      {partnerActive ? (
        <Tabs value={tab} onValueChange={(value) => setTab(value === "partner" ? "partner" : "mine")}>
          <TabsList>
            <TabsTrigger value="mine">Mine</TabsTrigger>
            <TabsTrigger value="partner">Partner&apos;s</TabsTrigger>
          </TabsList>
          <TabsContent value="mine" className="mt-4">
            <MealList scope="mine" />
          </TabsContent>
          <TabsContent value="partner" className="mt-4">
            <MealList scope="partner" />
          </TabsContent>
        </Tabs>
      ) : (
        <MealList scope="mine" />
      )}
    </div>
  );
}
