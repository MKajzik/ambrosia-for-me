"use client";

import { ListPlus, Plus } from "lucide-react";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { usePartnerLink } from "@/features/meals/queries";
import { GenerateListDialog } from "./generate-list-dialog";
import { NewListDialog } from "./new-list-dialog";
import { ShoppingLists } from "./shopping-lists";

/** My shopping lists, and, while I have a partner, the ones they share. */
export function ShoppingPage() {
  const link = usePartnerLink();
  const [tab, setTab] = useState<"mine" | "partner">("mine");
  const [creating, setCreating] = useState(false);
  const [generating, setGenerating] = useState(false);
  const partnerActive = link.data?.status === "active";

  const generateButton = (
    <Button type="button" onClick={() => setGenerating(true)}>
      <ListPlus aria-hidden />
      Generate from plan
    </Button>
  );

  return (
    <div className="grid gap-4">
      <div className="flex flex-wrap justify-end gap-2">
        <Button type="button" variant="outline" onClick={() => setCreating(true)}>
          <Plus aria-hidden />
          New list
        </Button>
        {generateButton}
      </div>
      {partnerActive ? (
        <Tabs value={tab} onValueChange={(value) => setTab(value === "partner" ? "partner" : "mine")}>
          <TabsList>
            <TabsTrigger value="mine">Mine</TabsTrigger>
            <TabsTrigger value="partner">Partner&apos;s</TabsTrigger>
          </TabsList>
          <TabsContent value="mine" className="mt-4">
            <ShoppingLists scope="mine" emptyAction={generateButton} />
          </TabsContent>
          <TabsContent value="partner" className="mt-4">
            <ShoppingLists scope="partner" />
          </TabsContent>
        </Tabs>
      ) : (
        <ShoppingLists scope="mine" emptyAction={generateButton} />
      )}
      <NewListDialog open={creating} onOpenChange={setCreating} />
      <GenerateListDialog open={generating} onOpenChange={setGenerating} />
    </div>
  );
}
