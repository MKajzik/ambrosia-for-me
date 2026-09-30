import { Card, CardContent } from "@/components/ui/card";

/** Placeholder body for an area whose screens land in a later plan. */
export function ComingSoon({ children }: { children: React.ReactNode }) {
  return (
    <Card>
      <CardContent className="text-muted-foreground py-10 text-center text-sm">{children}</CardContent>
    </Card>
  );
}
