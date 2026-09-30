"use client";

import { ArrowLeft } from "lucide-react";
import Link from "next/link";
import { ErrorState } from "@/components/error-state";
import { PageHeader } from "@/components/page-header";
import { Skeleton } from "@/components/ui/skeleton";
import { ApiError, problemMessage } from "@/lib/api/problem";
import { TemplateEditor } from "./template-editor";
import { useTemplate } from "./template-queries";
import { TemplateView } from "./template-view";

function BackToTemplates() {
  return (
    <Link href="/plan/templates" className="text-muted-foreground hover:text-foreground mb-4 inline-flex items-center gap-1 text-sm">
      <ArrowLeft aria-hidden className="size-4" />
      Diet templates
    </Link>
  );
}

/** Loads one template: mine opens in the editor, the partner's in a read-only view. */
export function TemplateDetail({ id }: { id: string }) {
  const query = useTemplate(id);

  // A background refetch that fails must not replace an editor that already has the template on screen.
  if (query.data === undefined) {
    return (
      <>
        <BackToTemplates />
        {query.isPending ? (
          <div role="status" className="grid gap-3" aria-label="Loading template">
            <Skeleton className="h-9 w-48" />
            <Skeleton className="h-40 rounded-xl" />
          </div>
        ) : (
          <ErrorState message={problemMessage(query.error)} onRetry={query.error instanceof ApiError && query.error.status === 404 ? undefined : () => void query.refetch()} />
        )}
      </>
    );
  }

  const template = query.data;
  return (
    <>
      <BackToTemplates />
      <PageHeader title={template.is_owner ? "Edit template" : template.name} />
      {template.is_owner ? <TemplateEditor key={template.id} template={template} /> : <TemplateView template={template} />}
    </>
  );
}
