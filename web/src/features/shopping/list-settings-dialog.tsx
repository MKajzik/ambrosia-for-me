"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { toast } from "sonner";
import { Field } from "@/components/field";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { usePartnerLink } from "@/features/meals/queries";
import { problemMessage } from "@/lib/api/problem";
import { rangeLabel, type ShoppingList } from "./items";
import { useDeleteShoppingList, useGenerateShoppingList, useUpdateShoppingList } from "./queries";

type Props = { list: ShoppingList; open: boolean; onOpenChange: (open: boolean) => void };

/** The owner's controls for a list: rename, share, regenerate from the plan, delete. The partner never sees this. */
export function ListSettingsDialog({ list, open, onOpenChange }: Props) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>List settings</DialogTitle>
          <DialogDescription>Only you can change these. Your partner can still tick items off and add to a shared list.</DialogDescription>
        </DialogHeader>
        <SettingsBody list={list} onDone={() => onOpenChange(false)} />
      </DialogContent>
    </Dialog>
  );
}

/** Mounted only while the dialog is open, so every open starts from the list as it is. */
function SettingsBody({ list, onDone }: { list: ShoppingList; onDone: () => void }) {
  const router = useRouter();
  const partner = usePartnerLink();
  const update = useUpdateShoppingList(list.id);
  const generate = useGenerateShoppingList();
  const remove = useDeleteShoppingList();
  const [shared, setShared] = useState(list.shared_with_partner);
  const [nameError, setNameError] = useState<string | undefined>();
  const [step, setStep] = useState<"idle" | "regenerate" | "delete">("idle");
  const [failure, setFailure] = useState<string | null>(null);

  const canShare = partner.data?.status === "active" || list.shared_with_partner;
  const range = rangeLabel(list);

  return (
    <div className="grid gap-6">
      <form
        noValidate
        className="grid gap-4"
        onSubmit={(event) => {
          event.preventDefault();
          const name = String(new FormData(event.currentTarget).get("list-name") ?? "").trim();
          if (!name) return setNameError("Give the list a name.");
          if (name.length > 200) return setNameError("Use at most 200 characters.");
          setNameError(undefined);
          const changes = { ...(name !== list.name ? { name } : {}), ...(shared !== list.shared_with_partner ? { shared_with_partner: shared } : {}) };
          if (Object.keys(changes).length === 0) return onDone();
          update.mutate(changes, {
            onSuccess: () => {
              toast.success("List saved.");
              onDone();
            },
            onError: (error) => toast.error(problemMessage(error)),
          });
        }}
      >
        <Field name="list-name" label="Name" autoComplete="off" defaultValue={list.name} error={nameError} />
        {canShare ? (
          <label className="flex items-center gap-2 text-sm">
            <input type="checkbox" className="accent-primary size-4" checked={shared} onChange={(event) => setShared(event.target.checked)} />
            Share with my partner
          </label>
        ) : null}
        <div>
          <Button type="submit" disabled={update.isPending}>
            {update.isPending ? "Saving…" : "Save"}
          </Button>
        </div>
      </form>

      {range ? (
        <section className="grid gap-2 border-t pt-4">
          <p className="text-sm">Built from your plan for {range}.</p>
          {step === "regenerate" ? (
            <div className="grid gap-2 text-sm">
              <p>
                Regenerating replaces the items generated from your plan with the plan as it is now. Items you added yourself stay. Checks on generated items are lost.
              </p>
              <div className="flex gap-2">
                <Button
                  type="button"
                  disabled={generate.isPending}
                  onClick={() => {
                    setFailure(null);
                    generate.mutate(
                      { from: list.source_from ?? "", to: list.source_to ?? "", listId: list.id },
                      {
                        onSuccess: () => {
                          toast.success("List regenerated.");
                          onDone();
                        },
                        onError: (error) => setFailure(problemMessage(error)),
                      },
                    );
                  }}
                >
                  {generate.isPending ? "Regenerating…" : "Regenerate"}
                </Button>
                <Button type="button" variant="outline" onClick={() => setStep("idle")}>
                  Cancel
                </Button>
              </div>
            </div>
          ) : (
            <div>
              <Button type="button" variant="outline" onClick={() => setStep("regenerate")}>
                Regenerate from plan
              </Button>
            </div>
          )}
        </section>
      ) : null}

      <section className="grid gap-2 border-t pt-4">
        {step === "delete" ? (
          <div className="grid gap-2 text-sm">
            <p>Delete “{list.name}” and all its items? This can&apos;t be undone.</p>
            <div className="flex gap-2">
              <Button
                type="button"
                variant="destructive"
                disabled={remove.isPending}
                onClick={() => {
                  setFailure(null);
                  remove.mutate(list.id, {
                    onSuccess: () => {
                      toast.success("List deleted.");
                      router.replace("/shopping");
                    },
                    onError: (error) => setFailure(problemMessage(error)),
                  });
                }}
              >
                {remove.isPending ? "Deleting…" : "Delete list"}
              </Button>
              <Button type="button" variant="outline" onClick={() => setStep("idle")}>
                Cancel
              </Button>
            </div>
          </div>
        ) : (
          <div>
            <Button type="button" variant="destructive" onClick={() => setStep("delete")}>
              Delete list
            </Button>
          </div>
        )}
      </section>

      {failure ? (
        <p role="alert" className="text-destructive text-sm">
          {failure}
        </p>
      ) : null}
    </div>
  );
}
