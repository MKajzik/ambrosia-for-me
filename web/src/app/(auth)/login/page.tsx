import type { Metadata } from "next";
import { LoginForm } from "@/features/auth/login-form";
import { safeNext } from "@/lib/safe-next";

export const metadata: Metadata = { title: "Sign in" };

export default async function Page({ searchParams }: { searchParams: Promise<{ next?: string }> }) {
  const { next } = await searchParams;
  return (
    <>
      <h1 className="text-center text-2xl font-semibold tracking-tight">Sign in</h1>
      <LoginForm next={safeNext(next)} />
    </>
  );
}
