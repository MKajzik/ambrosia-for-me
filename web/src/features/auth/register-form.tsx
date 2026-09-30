"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { Field } from "@/components/field";
import { Button } from "@/components/ui/button";
import { ApiError, problemMessage } from "@/lib/api/problem";
import { ME_KEY } from "./use-me";
import { register } from "./session";

export function RegisterForm() {
  const router = useRouter();
  const queryClient = useQueryClient();
  const mutation = useMutation({
    mutationFn: register,
    onSuccess: (user) => {
      queryClient.clear();
      queryClient.setQueryData(ME_KEY, user);
      router.replace("/today");
    },
  });
  const error = mutation.error;
  const fieldErrors = error instanceof ApiError ? error.fieldErrors : {};
  // A taken email is a field problem the person can fix, so it is shown at the field, not in the banner.
  const emailTaken = error instanceof ApiError && error.code === "email_taken";
  const emailError = fieldErrors.email ?? (emailTaken ? problemMessage(error) : undefined);

  return (
    <form
      noValidate
      className="grid gap-4"
      onSubmit={(e) => {
        e.preventDefault();
        const data = new FormData(e.currentTarget);
        mutation.mutate({
          display_name: String(data.get("display_name") ?? ""),
          email: String(data.get("email") ?? ""),
          password: String(data.get("password") ?? ""),
        });
      }}
    >
      {error && !emailTaken ? (
        <p role="alert" className="bg-destructive/10 text-destructive rounded-lg px-3 py-2 text-sm">
          {problemMessage(error)}
        </p>
      ) : null}
      <Field name="display_name" label="Your name" autoComplete="name" error={fieldErrors.display_name} />
      <Field name="email" label="Email" type="email" autoComplete="email" error={emailError} />
      <Field name="password" label="Password" type="password" autoComplete="new-password" hint="At least 10 characters." error={fieldErrors.password} />
      <Button type="submit" size="lg" className="h-11 text-base md:text-sm" disabled={mutation.isPending}>
        {mutation.isPending ? "Creating account…" : "Create account"}
      </Button>
      <p className="text-muted-foreground text-center text-sm">
        Already have an account?{" "}
        <Link href="/login" className="text-primary font-medium underline-offset-4 hover:underline">
          Sign in
        </Link>
      </p>
    </form>
  );
}
