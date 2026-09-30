import type { Metadata } from "next";
import { RegisterForm } from "@/features/auth/register-form";

export const metadata: Metadata = { title: "Create account" };

export default function Page() {
  return (
    <>
      <h1 className="text-center text-2xl font-semibold tracking-tight">Create your account</h1>
      <RegisterForm />
    </>
  );
}
