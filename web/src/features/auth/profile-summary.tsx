"use client";

import { useMutation } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { problemMessage } from "@/lib/api/problem";
import { hardNavigate } from "@/lib/navigation";
import { logout } from "./session";
import { useMe } from "./use-me";

/** Who is signed in, and the way out. The rest of Profile arrives with its own plan. */
export function ProfileSummary() {
  const me = useMe();
  const signOut = useMutation({
    mutationFn: logout,
    // A full page load, not a client-side route change: it wipes the cache and cannot race the
    // profile query, which would otherwise refetch, get a 401 and bounce to /login?next=/profile.
    onSettled: () => hardNavigate("/login"),
  });

  return (
    <Card>
      <CardContent className="grid gap-4 py-6">
        {me.isPending ? (
          <p className="text-muted-foreground text-sm">Loading…</p>
        ) : me.isError ? (
          <p role="alert" className="text-destructive text-sm">
            {problemMessage(me.error)}
          </p>
        ) : (
          <div>
            <p className="text-lg font-medium">{me.data.display_name}</p>
            <p className="text-muted-foreground text-sm">{me.data.email}</p>
          </div>
        )}
        <div>
          <Button variant="outline" size="lg" className="h-11" onClick={() => signOut.mutate()} disabled={signOut.isPending}>
            Sign out
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}
