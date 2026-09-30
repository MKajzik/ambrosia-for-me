import { cn } from "cn";

/** A plain `<select>`, styled like `Input`. Native selects give phones and screen readers their own good pickers. */
export function NativeSelect({ className, ...props }: React.ComponentProps<"select">) {
  return (
    <select
      className={cn(
        "border-input bg-background text-foreground focus-visible:border-ring focus-visible:ring-ring/50 dark:bg-input/30 h-11 rounded-lg border px-3 text-base outline-none focus-visible:ring-3 disabled:opacity-50 md:text-sm",
        className,
      )}
      {...props}
    />
  );
}
