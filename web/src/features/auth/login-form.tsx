"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { Field } from "@/components/field";
import { Button } from "@/components/ui/button";
import { ApiError, problemMessage } from "@/lib/api/problem";
import { ME_KEY } from "./use-me";
import { login } from "./session";

export function LoginForm({ next }: { next: string }) {
  const router = useRouter();
  const queryClient = useQueryClient();
  const mutation = useMutation({
    mutationFn: login,
    onSuccess: (user) => {
      queryClient.clear();
      queryClient.setQueryData(ME_KEY, user);
      router.replace(next);
    },
  });
  const error = mutation.error;
  const fieldErrors = error instanceof ApiError ? error.fieldErrors : {};

  return (
    <form
      noValidate
      className="grid gap-4"
      onSubmit={(e) => {
        e.preventDefault();
        const data = new FormData(e.currentTarget);
        mutation.mutate({ email: String(data.get("email") ?? ""), password: String(data.get("password") ?? "") });
      }}
    >
      {error ? (
        <p role="alert" className="bg-destructive/10 text-destructive rounded-lg px-3 py-2 text-sm">
          {problemMessage(error)}
        </p>
      ) : null}
      <Field name="email" label="Email" type="email" autoComplete="email" error={fieldErrors.email} />
      <Field name="password" label="Password" type="password" autoComplete="current-password" error={fieldErrors.password} />
      <Button type="submit" size="lg" className="h-11 text-base md:text-sm" disabled={mutation.isPending}>
        {mutation.isPending ? "Signing in…" : "Sign in"}
      </Button>
      <p className="text-muted-foreground text-center text-sm">
        New here?{" "}
        <Link href="/register" className="text-primary font-medium underline-offset-4 hover:underline">
          Create an account
        </Link>
      </p>
    </form>
  );
}
