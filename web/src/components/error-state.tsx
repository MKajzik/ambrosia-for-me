import { Button } from "@/components/ui/button";

/** A load failure the person can retry. */
export function ErrorState({ message, onRetry }: { message: string; onRetry?: () => void }) {
  return (
    <div role="alert" className="bg-destructive/10 text-destructive flex flex-wrap items-center gap-3 rounded-xl px-4 py-3 text-sm">
      <p className="flex-1">{message}</p>
      {onRetry ? (
        <Button type="button" variant="outline" size="sm" onClick={onRetry}>
          Try again
        </Button>
      ) : null}
    </div>
  );
}
