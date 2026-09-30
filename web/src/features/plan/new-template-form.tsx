"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { toast } from "sonner";
import { Field } from "@/components/field";
import { Button } from "@/components/ui/button";
import { problemMessage } from "@/lib/api/problem";
import { parseDecimal } from "@/lib/parse-number";
import { useCreateTemplate } from "./template-queries";

/** The two things the API needs to make a template. Its meals are edited (and autosaved) on the template's own page. */
export function NewTemplateForm() {
  const router = useRouter();
  const create = useCreateTemplate();
  const [errors, setErrors] = useState<{ name?: string; days?: string }>({});

  return (
    <form
      noValidate
      className="grid max-w-md gap-4"
      onSubmit={(event) => {
        event.preventDefault();
        const data = new FormData(event.currentTarget);
        const name = String(data.get("new-template-name") ?? "").trim();
        const parsed = parseDecimal(String(data.get("new-template-days") ?? ""));
        const days = parsed.ok ? parsed.value : null;
        const next: { name?: string; days?: string } = {};
        if (!name) next.name = "Give the template a name.";
        else if (name.length > 200) next.name = "Use at most 200 characters.";
        if (days === null || !Number.isInteger(days) || days < 1 || days > 31) next.days = "Days must be a whole number from 1 to 31.";
        setErrors(next);
        if (next.name || next.days || days === null) return;
        create.mutate({ name, day_count: days }, { onSuccess: (template) => router.replace(`/plan/templates/${template.id}`), onError: (error) => toast.error(problemMessage(error)) });
      }}
    >
      <Field name="new-template-name" label="Name" autoComplete="off" error={errors.name} />
      <Field name="new-template-days" label="Number of days" inputMode="numeric" defaultValue="7" hint="7 makes a week. It cannot be changed later." error={errors.days} />
      <Button type="submit" size="lg" className="h-11 text-base md:text-sm" disabled={create.isPending}>
        {create.isPending ? "Creating…" : "Create template"}
      </Button>
    </form>
  );
}
