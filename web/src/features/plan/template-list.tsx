"use client";

import { useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useEffect } from "react";
import { ErrorState } from "@/components/error-state";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { PARTNER_KEY } from "@/features/meals/queries";
import { ApiError, problemMessage } from "@/lib/api/problem";
import { CopyTemplateButton } from "./copy-template-button";
import { useTemplates, type DietTemplateSummary } from "./template-queries";

type Scope = "mine" | "partner";

export function daysLabel(days: number): string {
  return `${days} ${days === 1 ? "day" : "days"}`;
}

export function TemplateList({ scope }: { scope: Scope }) {
  const query = useTemplates(scope);
  const queryClient = useQueryClient();
  const unlinked = query.error instanceof ApiError && query.error.code === "partner_not_linked";

  // The partner unlinked while their tab was open: refresh the link so the tab goes away.
  useEffect(() => {
    if (unlinked) void queryClient.invalidateQueries({ queryKey: PARTNER_KEY });
  }, [unlinked, queryClient]);

  if (query.isPending) {
    return (
      <ul aria-label="Loading templates" className="grid gap-3">
        {[0, 1, 2].map((n) => (
          <li key={n}>
            <Skeleton className="h-[4.5rem] rounded-xl" />
          </li>
        ))}
      </ul>
    );
  }
  if (query.data === undefined) return <ErrorState message={problemMessage(query.error)} onRetry={unlinked ? undefined : () => void query.refetch()} />;

  const templates = query.data.pages.flatMap((page) => page.items);
  if (templates.length === 0) return <EmptyState scope={scope} />;

  return (
    <div className="grid gap-3">
      <ul className="grid gap-3">
        {templates.map((template) => (
          <TemplateRow key={template.id} template={template} scope={scope} />
        ))}
      </ul>
      {query.isError ? <ErrorState message={problemMessage(query.error)} onRetry={() => void query.fetchNextPage()} /> : null}
      {query.hasNextPage ? (
        <Button type="button" variant="outline" className="justify-self-center" disabled={query.isFetchingNextPage} onClick={() => void query.fetchNextPage()}>
          {query.isFetchingNextPage ? "Loading…" : "Load more"}
        </Button>
      ) : null}
    </div>
  );
}

function TemplateRow({ template, scope }: { template: DietTemplateSummary; scope: Scope }) {
  return (
    <li className="bg-card flex items-center gap-3 rounded-xl border p-4">
      <Link href={`/plan/templates/${template.id}`} className="focus-visible:ring-ring/50 min-w-0 flex-1 rounded-md outline-none focus-visible:ring-3">
        <span className="block truncate font-medium">{template.name}</span>
        <span className="text-muted-foreground block text-sm">{daysLabel(template.day_count)}</span>
      </Link>
      {scope === "mine" && template.shared_with_partner ? <Badge variant="secondary">Shared</Badge> : null}
      {scope === "partner" ? <CopyTemplateButton templateId={template.id} templateName={template.name} /> : null}
    </li>
  );
}

function EmptyState({ scope }: { scope: Scope }) {
  return (
    <div className="bg-card flex flex-col items-center gap-3 rounded-xl border px-6 py-10 text-center">
      <p className="font-medium">{scope === "mine" ? "No diet templates yet" : "Nothing shared yet"}</p>
      <p className="text-muted-foreground max-w-sm text-sm">
        {scope === "mine"
          ? "A template is a reusable schedule of meals. Build one, then apply it to any week of your plan."
          : "Templates your partner shares with you show up here, and you can copy them into your library."}
      </p>
      {scope === "mine" ? (
        <Button asChild>
          <Link href="/plan/templates/new">Create your first template</Link>
        </Button>
      ) : null}
    </div>
  );
}
