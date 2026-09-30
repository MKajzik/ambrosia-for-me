"use client";

import { Copy } from "lucide-react";
import { useRouter } from "next/navigation";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { problemMessage } from "@/lib/api/problem";
import { useCopyMeal } from "./queries";

/** Copies a meal (usually the partner's) into the caller's own library and opens the copy. */
export function CopyMealButton({ mealId, mealName, variant = "outline" }: { mealId: string; mealName: string; variant?: "outline" | "default" }) {
  const router = useRouter();
  const copy = useCopyMeal();
  return (
    <Button
      type="button"
      variant={variant}
      size="sm"
      disabled={copy.isPending}
      onClick={() =>
        copy.mutate(mealId, {
          onSuccess: (meal) => {
            toast.success("Copied to your library.");
            router.push(`/meals/${meal.id}`);
          },
          onError: (error) => toast.error(problemMessage(error)),
        })
      }
    >
      <Copy aria-hidden />
      {copy.isPending ? "Copying…" : "Copy to my library"}
      <span className="sr-only"> ({mealName})</span>
    </Button>
  );
}
