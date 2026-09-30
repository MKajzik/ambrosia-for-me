import type { Metadata } from "next";
import { PageHeader } from "@/components/page-header";
import { ProfileSummary } from "@/features/auth/profile-summary";

export const metadata: Metadata = { title: "Profile" };

export default function Page() {
  return (
    <>
      <PageHeader title="Profile" />
      <ProfileSummary />
    </>
  );
}
