"use client";

import { useState } from "react";
import { toast } from "sonner";
import { ErrorState } from "@/components/error-state";
import { Field } from "@/components/field";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Skeleton } from "@/components/ui/skeleton";
import { usePartnerLink, type Partnership } from "@/features/meals/queries";
import { ApiError, problemMessage } from "@/lib/api/problem";
import { formatDate, formatInviteCode, formatWhen } from "./invite-code";
import { useAcceptInvite, useCreateInvite, useUnlinkPartner } from "./queries";

type Invite = { code: string; expires_at: string };

/** Link with one partner by an invite code, see the link, or end it. */
export function PartnerCard() {
  const link = usePartnerLink();
  // The code is returned once, when it is made; hold it here so the pending view can still show it.
  const [invite, setInvite] = useState<Invite | null>(null);

  return (
    <Card>
      <CardHeader>
        <CardTitle>Partner</CardTitle>
      </CardHeader>
      <CardContent className="grid gap-4">
        <p className="text-muted-foreground text-sm">Link with one partner to share meals, diet templates and live shopping lists.</p>
        {link.isPending ? (
          <Skeleton role="status" aria-label="Loading partner" className="h-24 rounded-lg" />
        ) : link.isError ? (
          <ErrorState message={problemMessage(link.error)} onRetry={() => void link.refetch()} />
        ) : link.data === null ? (
          <div className="grid gap-6 sm:grid-cols-2">
            <InviteSection invite={invite} onCreated={setInvite} />
            <AcceptForm />
          </div>
        ) : link.data.status === "pending" ? (
          <div className="grid gap-6 sm:grid-cols-2">
            <PendingSection link={link.data} invite={invite} onCreated={setInvite} onCancelled={() => setInvite(null)} />
            <AcceptForm />
          </div>
        ) : (
          <Linked link={link.data} />
        )}
      </CardContent>
    </Card>
  );
}

function CodeBox({ invite }: { invite: Invite }) {
  async function copy() {
    try {
      await navigator.clipboard.writeText(invite.code);
      toast.success("Code copied.");
    } catch {
      toast.error("Couldn't copy. Select the code and copy it by hand.");
    }
  }
  return (
    <div className="grid gap-2">
      <p className="bg-muted rounded-lg px-4 py-3 font-mono text-2xl tracking-widest select-all">{formatInviteCode(invite.code)}</p>
      <p className="text-muted-foreground text-xs">Shown once. Share it with your partner. It expires {formatWhen(invite.expires_at)}.</p>
      <div>
        <Button type="button" variant="outline" size="sm" onClick={() => void copy()}>
          Copy code
        </Button>
      </div>
    </div>
  );
}

function InviteSection({ invite, onCreated }: { invite: Invite | null; onCreated: (invite: Invite) => void }) {
  const create = useCreateInvite();
  return (
    <section className="grid content-start gap-3">
      <h3 className="text-sm font-medium">Invite your partner</h3>
      {invite ? <CodeBox invite={invite} /> : null}
      <div>
        <Button type="button" disabled={create.isPending} onClick={() => create.mutate(undefined, { onSuccess: onCreated, onError: (error) => toast.error(problemMessage(error)) })}>
          {create.isPending ? "Creating…" : "Create invite code"}
        </Button>
      </div>
    </section>
  );
}

function PendingSection({ link, invite, onCreated, onCancelled }: { link: Partnership; invite: Invite | null; onCreated: (invite: Invite) => void; onCancelled: () => void }) {
  const create = useCreateInvite();
  const cancel = useUnlinkPartner();
  return (
    <section className="grid content-start gap-3">
      <h3 className="text-sm font-medium">Invite pending</h3>
      <p className="text-sm">Waiting for your partner. The code expires {link.expires_at ? formatWhen(link.expires_at) : "soon"}.</p>
      {invite ? <CodeBox invite={invite} /> : <p className="text-muted-foreground text-xs">The code was shown when you created it. Create a new code if you need to see it again.</p>}
      <div className="flex flex-wrap gap-2">
        <Button type="button" variant="outline" disabled={create.isPending} onClick={() => create.mutate(undefined, { onSuccess: onCreated, onError: (error) => toast.error(problemMessage(error)) })}>
          Create a new code
        </Button>
        <Button type="button" variant="ghost" disabled={cancel.isPending} onClick={() => cancel.mutate(undefined, { onSuccess: onCancelled, onError: (error) => toast.error(problemMessage(error)) })}>
          Cancel invite
        </Button>
      </div>
    </section>
  );
}

function AcceptForm() {
  const accept = useAcceptInvite();
  const [error, setError] = useState<string | undefined>();
  return (
    <section className="grid content-start gap-3">
      <h3 className="text-sm font-medium">Enter a code</h3>
      <form
        noValidate
        className="grid gap-3"
        onSubmit={(event) => {
          event.preventDefault();
          const code = String(new FormData(event.currentTarget).get("invite-code") ?? "").trim();
          if (!code) return setError("Enter the code your partner sent you.");
          setError(undefined);
          accept.mutate(code, {
            onSuccess: (partnership) => toast.success(`Linked with ${partnership.display_name ?? "your partner"}.`),
            onError: (e) => (e instanceof ApiError && (e.code === "invite_invalid" || e.code === "partner_already_linked") ? setError(problemMessage(e)) : toast.error(problemMessage(e))),
          });
        }}
      >
        <Field name="invite-code" label="Invite code" autoComplete="off" autoCapitalize="characters" error={error} />
        <div>
          <Button type="submit" disabled={accept.isPending}>
            {accept.isPending ? "Linking…" : "Link accounts"}
          </Button>
        </div>
      </form>
    </section>
  );
}

function Linked({ link }: { link: Partnership }) {
  const [open, setOpen] = useState(false);
  const unlink = useUnlinkPartner();
  const name = link.display_name ?? "your partner";
  return (
    <div className="grid gap-3">
      <p className="text-sm">
        Linked with {name}
        {link.linked_at ? ` since ${formatDate(link.linked_at)}` : ""}.
      </p>
      <div>
        <Button type="button" variant="outline" onClick={() => setOpen(true)}>
          Unlink
        </Button>
      </div>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Unlink from {name}?</DialogTitle>
            <DialogDescription>
              You will stop seeing each other&apos;s shared meals, diet templates and shopping lists, and a shared list that is open will close. Copies stay with whoever made them. You need a new invite to link again.
            </DialogDescription>
          </DialogHeader>
          {unlink.error ? (
            <p role="alert" className="text-destructive text-sm">
              {problemMessage(unlink.error)}
            </p>
          ) : null}
          <div className="flex flex-wrap gap-2">
            <Button type="button" variant="destructive" disabled={unlink.isPending} onClick={() => unlink.mutate(undefined, { onSuccess: () => toast.success("Unlinked.") })}>
              {unlink.isPending ? "Unlinking…" : "Unlink"}
            </Button>
            <Button type="button" variant="outline" onClick={() => setOpen(false)}>
              Keep the link
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}
