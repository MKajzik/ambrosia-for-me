"use client";

import { Copy } from "lucide-react";
import { useRouter } from "next/navigation";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { problemMessage } from "@/lib/api/problem";
import { useCopyTemplate } from "./template-queries";

/** Copies a template (usually the partner's) into my own library and opens the copy. Only a copy can be edited or applied. */
export function CopyTemplateButton({ templateId, templateName, variant = "outline" }: { templateId: string; templateName: string; variant?: "outline" | "default" }) {
  const router = useRouter();
  const copy = useCopyTemplate();
  return (
    <Button
      type="button"
      variant={variant}
      size="sm"
      disabled={copy.isPending}
      onClick={() =>
        copy.mutate(templateId, {
          onSuccess: (template) => {
            toast.success("Copied to your library.");
            router.push(`/plan/templates/${template.id}`);
          },
          onError: (error) => toast.error(problemMessage(error)),
        })
      }
    >
      <Copy aria-hidden />
      {copy.isPending ? "Copying…" : "Copy to my library"}
      <span className="sr-only"> ({templateName})</span>
    </Button>
  );
}
