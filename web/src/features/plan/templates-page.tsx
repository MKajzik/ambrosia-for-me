"use client";

import { ArrowLeft, Plus } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { usePartnerLink } from "@/features/meals/queries";
import { TemplateList } from "./template-list";

/** My diet templates, and, while I have a partner, the ones they share. */
export function TemplatesPage() {
  const link = usePartnerLink();
  const [tab, setTab] = useState<"mine" | "partner">("mine");
  const partnerActive = link.data?.status === "active";

  return (
    <div className="grid gap-4">
      <div className="flex items-center justify-between gap-3">
        <Link href="/plan" className="text-muted-foreground hover:text-foreground inline-flex items-center gap-1 text-sm">
          <ArrowLeft aria-hidden className="size-4" />
          Plan
        </Link>
        <Button asChild>
          <Link href="/plan/templates/new">
            <Plus aria-hidden />
            New template
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
            <TemplateList scope="mine" />
          </TabsContent>
          <TabsContent value="partner" className="mt-4">
            <TemplateList scope="partner" />
          </TabsContent>
        </Tabs>
      ) : (
        <TemplateList scope="mine" />
      )}
    </div>
  );
}
