import type { Metadata } from "next";
import Link from "next/link";
import { PageHeader } from "@/components/page-header";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { ProfileSummary } from "@/features/auth/profile-summary";
import { PartnerCard } from "@/features/partner/partner-card";
import { DeleteAccount } from "@/features/profile/delete-account";
import { TargetsForm } from "@/features/profile/targets-form";

export const metadata: Metadata = { title: "Profile" };

export default function Page() {
  return (
    <>
      <PageHeader title="Profile" />
      <div className="grid max-w-3xl gap-6">
        <ProfileSummary />
        <TargetsForm />
        <PartnerCard />
        <Card>
          <CardHeader>
            <CardTitle>Custom ingredients</CardTitle>
          </CardHeader>
          <CardContent className="grid gap-3">
            <p className="text-muted-foreground text-sm">Ingredients you made yourself: edit their nutrition or delete them.</p>
            <div>
              <Button asChild variant="outline">
                <Link href="/profile/ingredients">Manage custom ingredients</Link>
              </Button>
            </div>
          </CardContent>
        </Card>
        <DeleteAccount />
      </div>
    </>
  );
}
