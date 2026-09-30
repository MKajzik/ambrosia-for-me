"use client";

import Link from "next/link";
import { useState } from "react";
import { toast } from "sonner";
import { Field } from "@/components/field";
import { NativeSelect } from "@/components/native-select";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { ApiError, problemMessage } from "@/lib/api/problem";
import { isIsoDate } from "@/lib/dates";
import { useLoadAllPages } from "@/lib/use-load-all-pages";
import { useApplyTemplate } from "./queries";
import { useTemplates } from "./template-queries";

type Props = { open: boolean; onOpenChange: (open: boolean) => void; defaultStart: string; onApplied: (startDate: string) => void };

export function ApplyTemplateDialog({ open, onOpenChange, defaultStart, onApplied }: Props) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Apply a diet template</DialogTitle>
          <DialogDescription>Copies the template into your plan. After that, changing a day never changes the template.</DialogDescription>
        </DialogHeader>
        <ApplyForm defaultStart={defaultStart} onApplied={onApplied} onDone={() => onOpenChange(false)} />
      </DialogContent>
    </Dialog>
  );
}

/** Mounted only while the dialog is open, so it starts fresh every time. */
function ApplyForm({ defaultStart, onApplied, onDone }: { defaultStart: string; onApplied: (startDate: string) => void; onDone: () => void }) {
  const templates = useTemplates("mine");
  useLoadAllPages(templates);
  const apply = useApplyTemplate();
  const [templateId, setTemplateId] = useState("");
  const [start, setStart] = useState(defaultStart);
  const [problem, setProblem] = useState<string | null>(null);
  const [conflict, setConflict] = useState(false);

  if (templates.isPending) return <Skeleton role="status" aria-label="Loading templates" className="h-24 rounded-lg" />;
  const items = templates.data?.pages.flatMap((page) => page.items) ?? [];
  if (items.length === 0) {
    return (
      <p className="text-muted-foreground text-sm">
        You have no diet templates yet.{" "}
        <Link href="/plan/templates/new" className="text-primary font-medium underline-offset-4 hover:underline">
          Create a template
        </Link>
      </p>
    );
  }
  const chosen = templateId || items[0]?.id || "";

  function submit(overwrite: boolean) {
    if (!isIsoDate(start)) {
      setProblem("Pick a start date.");
      return;
    }
    setProblem(null);
    apply.mutate(
      { templateId: chosen, startDate: start, overwrite },
      {
        onSuccess: () => {
          toast.success("Template applied.");
          onApplied(start);
          onDone();
        },
        onError: (error) => {
          if (error instanceof ApiError && error.code === "plan_conflict") setConflict(true);
          else toast.error(problemMessage(error));
        },
      },
    );
  }

  return (
    <form
      noValidate
      className="grid gap-4"
      onSubmit={(event) => {
        event.preventDefault();
        setConflict(false);
        submit(false);
      }}
    >
      <div className="grid gap-1.5">
        <Label htmlFor="apply-template">Template</Label>
        <NativeSelect
          id="apply-template"
          value={chosen}
          onChange={(event) => {
            setTemplateId(event.target.value);
            setConflict(false);
          }}
        >
          {items.map((template) => (
            <option key={template.id} value={template.id}>
              {template.name} ({template.day_count} {template.day_count === 1 ? "day" : "days"})
            </option>
          ))}
        </NativeSelect>
      </div>
      <Field
        name="apply-start"
        label="Start date"
        type="date"
        value={start}
        onChange={(event) => {
          setStart(event.target.value);
          setConflict(false);
        }}
        hint="Day 1 of the template lands on this date."
        error={problem ?? undefined}
      />
      {conflict ? (
        <div role="alert" className="bg-destructive/10 grid gap-2 rounded-lg p-3 text-sm">
          <p className="text-destructive">Some of those days already have meals. Replace them?</p>
          <p className="text-muted-foreground text-xs">Replacing swaps breakfast, lunch and dinner. Snacks are added to what is already there.</p>
          <div className="flex gap-2">
            <Button type="button" variant="destructive" size="sm" disabled={apply.isPending} onClick={() => submit(true)}>
              Replace them
            </Button>
            <Button type="button" variant="outline" size="sm" onClick={() => setConflict(false)}>
              Keep my plan
            </Button>
          </div>
        </div>
      ) : null}
      <p className="text-muted-foreground text-xs">To use your partner&apos;s template, copy it to your library first (Diet templates).</p>
      <Button type="submit" size="lg" className="h-11 text-base md:text-sm" disabled={apply.isPending || conflict}>
        {apply.isPending ? "Applying…" : "Apply template"}
      </Button>
    </form>
  );
}
