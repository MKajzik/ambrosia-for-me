"use client";

import { useState } from "react";
import { Field } from "@/components/field";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { useMe } from "@/features/auth/use-me";
import { problemMessage } from "@/lib/api/problem";
import { hardNavigate } from "@/lib/navigation";
import { useDeleteAccount } from "./queries";

/** The one action that cannot be undone. The API asks for no re-authentication, so the screen asks for the email itself. */
export function DeleteAccount() {
  const me = useMe();
  const [open, setOpen] = useState(false);
  return (
    <Card>
      <CardHeader>
        <CardTitle>Delete account</CardTitle>
      </CardHeader>
      <CardContent className="grid gap-3">
        <p className="text-muted-foreground text-sm">Permanently delete your account and everything in it.</p>
        <div>
          <Button type="button" variant="destructive" disabled={me.data === undefined} onClick={() => setOpen(true)}>
            Delete account
          </Button>
        </div>
      </CardContent>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete your account?</DialogTitle>
            <DialogDescription>
              This permanently deletes your account and everything it owns: your meals, diet templates, plan and shopping lists. A partner keeps only the copies they made themselves. It cannot be undone.
            </DialogDescription>
          </DialogHeader>
          {me.data ? <Confirm email={me.data.email} onCancel={() => setOpen(false)} /> : null}
        </DialogContent>
      </Dialog>
    </Card>
  );
}

/** Mounted only while the dialog is open, so the confirmation always starts empty. */
function Confirm({ email, onCancel }: { email: string; onCancel: () => void }) {
  const remove = useDeleteAccount();
  const [typed, setTyped] = useState("");
  const matches = typed.trim().toLowerCase() === email.toLowerCase();

  return (
    <div className="grid gap-4">
      <Field name="confirm-email" label={`Type ${email} to confirm`} autoComplete="off" value={typed} onChange={(event) => setTyped(event.target.value)} />
      {remove.error ? (
        <p role="alert" className="text-destructive text-sm">
          {problemMessage(remove.error)}
        </p>
      ) : null}
      <div className="flex flex-wrap gap-2">
        <Button
          type="button"
          variant="destructive"
          // A full page load, like signing out: it drops every cache with the deleted session instead of racing a query about to refetch.
          onClick={() => remove.mutate(undefined, { onSuccess: () => hardNavigate("/login") })}
          disabled={!matches || remove.isPending}
        >
          {remove.isPending ? "Deleting…" : "Delete my account"}
        </Button>
        <Button type="button" variant="outline" onClick={onCancel}>
          Keep my account
        </Button>
      </div>
    </div>
  );
}
