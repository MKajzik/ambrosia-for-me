import type { Metadata } from "next";
import { PageHeader } from "@/components/page-header";
import { ProfileSummary } from "@/features/auth/profile-summary";
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
        <DeleteAccount />
      </div>
    </>
  );
}
