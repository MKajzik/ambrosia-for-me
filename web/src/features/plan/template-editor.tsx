"use client";

import { Plus, Trash2, X } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import { Field } from "@/components/field";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { usePartnerLink, type MealSummary } from "@/features/meals/queries";
import { problemMessage } from "@/lib/api/problem";
import { useAutosave } from "@/lib/use-autosave";
import { MealPicker } from "./meal-picker";
import { SLOTS, SLOT_LABELS, type Slot } from "./plan-cache";
import { daysLabel } from "./template-list";
import { diffTemplate, draftFromTemplate, hasTemplateChanges, newSlotRow, savedFromTemplate, validateTemplate, type SlotRow, type TemplateDraft, type TemplateErrors, type ValidTemplate } from "./template-draft";
import { useDeleteTemplate, useTemplateActions, type DietTemplate } from "./template-queries";

type Picker = { dayIndex: number; slot: Slot; replaceKey: string | null };

/**
 * The owner's template editor. It autosaves like the meal editor: fields with `PATCH`, the whole slot list with `PUT`.
 * The draft is copied from `template` once, so a background refetch never overwrites what is being typed.
 */
export function TemplateEditor({ template, autosaveDelayMs }: { template: DietTemplate; autosaveDelayMs?: number }) {
  const router = useRouter();
  const partner = usePartnerLink();
  const actions = useTemplateActions(template.id);
  const deleteTemplate = useDeleteTemplate();

  const [draft, setDraft] = useState<TemplateDraft>(() => draftFromTemplate(template));
  const [saved, setSaved] = useState<ValidTemplate>(() => savedFromTemplate(template));
  const savedRef = useRef(saved);
  const commit = useCallback((next: ValidTemplate) => {
    savedRef.current = next;
    setSaved(next);
  }, []);
  const [picker, setPicker] = useState<Picker | null>(null);
  const [confirmingDelete, setConfirmingDelete] = useState(false);

  const validation = useMemo(() => validateTemplate(draft, template.day_count), [draft, template.day_count]);
  const value = validation.ok ? validation.value : null;
  const dirty = value !== null && hasTemplateChanges(saved, value);
  const errors = validation.ok ? null : validation.errors;

  // One save writes what changed: the fields first, then the slot list. Each step is committed on its own, so a failure in the
  // second leaves the first counted as saved and only the rest is retried.
  const save = useCallback(
    async (next: ValidTemplate) => {
      const { patch, items } = diffTemplate(savedRef.current, next);
      if (patch) {
        await actions.patch(patch);
        commit({ ...savedRef.current, name: next.name, shared: next.shared });
      }
      if (items) {
        await actions.replaceSlots(items);
        commit({ ...savedRef.current, items: next.items });
      }
    },
    [actions, commit],
  );
  const autosave = useAutosave({ value, dirty, save, delayMs: autosaveDelayMs });

  const unsaved = dirty || autosave.saving;
  useEffect(() => {
    if (!unsaved) return;
    const warn = (event: BeforeUnloadEvent) => event.preventDefault();
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [unsaved]);

  const chooseMeal = (meal: MealSummary) => {
    if (!picker) return;
    const { dayIndex, slot, replaceKey } = picker;
    setDraft((d) => ({
      ...d,
      rows: replaceKey ? d.rows.map((row) => (row.key === replaceKey ? { ...row, mealId: meal.id, mealName: meal.name } : row)) : [...d.rows, newSlotRow(dayIndex, slot, meal)],
    }));
  };
  const setPortion = (key: string, portion: string) => setDraft((d) => ({ ...d, rows: d.rows.map((row) => (row.key === key ? { ...row, portion } : row)) }));
  const removeRow = (key: string) => setDraft((d) => ({ ...d, rows: d.rows.filter((row) => row.key !== key) }));

  const canShare = partner.data?.status === "active" || draft.shared;
  const status = autosave.saving ? "Saving…" : errors ? "Fix the highlighted fields to save." : dirty ? "Unsaved changes" : "All changes saved";

  return (
    <div className="grid gap-6">
      <div className="flex flex-wrap items-center gap-3">
        <p role="status" className="text-muted-foreground text-sm">
          {status}
        </p>
        {autosave.error !== null && dirty ? (
          // Only while there is still something unsaved: putting the draft back to what the server holds clears the failure.
          <div role="alert" className="bg-destructive/10 text-destructive flex flex-wrap items-center gap-3 rounded-lg px-3 py-2 text-sm">
            <span>{problemMessage(autosave.error)}</span>
            <Button type="button" variant="outline" size="sm" onClick={() => void autosave.retry()}>
              Try again
            </Button>
          </div>
        ) : null}
      </div>

      <section aria-label="Template details" className="grid max-w-xl gap-4">
        <Field name="template-name" label="Name" autoComplete="off" value={draft.name} onChange={(e) => setDraft((d) => ({ ...d, name: e.target.value }))} error={errors?.name} />
        <p className="text-muted-foreground text-sm">
          {daysLabel(template.day_count)}
          <span className="sr-only"> (cannot be changed after creation)</span>
        </p>
        {canShare ? (
          <label className="flex items-center gap-2 text-sm">
            <input type="checkbox" className="accent-primary size-4" checked={draft.shared} onChange={(e) => setDraft((d) => ({ ...d, shared: e.target.checked }))} />
            Share with my partner
          </label>
        ) : null}
        {errors?.items ? <p className="text-destructive text-sm">{errors.items}</p> : null}
      </section>

      <div className="grid gap-4 lg:grid-cols-2">
        {Array.from({ length: template.day_count }, (_, dayIndex) => (
          <DaySection
            key={dayIndex}
            dayIndex={dayIndex}
            rows={draft.rows.filter((row) => row.dayIndex === dayIndex)}
            errors={errors}
            onAdd={(slot) => setPicker({ dayIndex, slot, replaceKey: null })}
            onSwap={(row) => setPicker({ dayIndex, slot: row.slot, replaceKey: row.key })}
            onPortion={setPortion}
            onRemove={removeRow}
          />
        ))}
      </div>

      <div>
        <Button
          type="button"
          variant="destructive"
          onClick={() => {
            deleteTemplate.reset();
            setConfirmingDelete(true);
          }}
        >
          <Trash2 aria-hidden />
          Delete template
        </Button>
      </div>

      <MealPicker
        open={picker !== null}
        onOpenChange={(open) => {
          if (!open) setPicker(null);
        }}
        title={picker ? (picker.slot === "snack" ? `Add a snack to day ${picker.dayIndex + 1}` : `Choose ${SLOT_LABELS[picker.slot].toLowerCase()} for day ${picker.dayIndex + 1}`) : "Choose a meal"}
        onPick={chooseMeal}
      />

      <Dialog open={confirmingDelete} onOpenChange={setConfirmingDelete}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete this template?</DialogTitle>
            <DialogDescription>“{template.name}” will be removed from your library. Meals already in your plan stay where they are.</DialogDescription>
          </DialogHeader>
          {deleteTemplate.error ? (
            <p role="alert" className="text-destructive text-sm">
              {problemMessage(deleteTemplate.error)}
            </p>
          ) : null}
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setConfirmingDelete(false)}>
              Keep it
            </Button>
            <Button
              type="button"
              variant="destructive"
              disabled={deleteTemplate.isPending}
              onClick={() =>
                deleteTemplate.mutate(template.id, {
                  onSuccess: () => {
                    toast.success("Template deleted.");
                    router.replace("/plan/templates");
                  },
                })
              }
            >
              {deleteTemplate.isPending ? "Deleting…" : "Delete template"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

type DayProps = {
  dayIndex: number;
  rows: SlotRow[];
  errors: TemplateErrors | null;
  onAdd: (slot: Slot) => void;
  onSwap: (row: SlotRow) => void;
  onPortion: (key: string, portion: string) => void;
  onRemove: (key: string) => void;
};

function DaySection({ dayIndex, rows, errors, onAdd, onSwap, onPortion, onRemove }: DayProps) {
  const n = dayIndex + 1;
  return (
    <section aria-label={`Day ${n}`} className="bg-card grid content-start gap-2 rounded-xl border p-4">
      <h2 className="font-semibold">Day {n}</h2>
      {SLOTS.map((slot) => {
        const label = SLOT_LABELS[slot];
        const lower = label.toLowerCase();
        const slotRows = rows.filter((row) => row.slot === slot);
        return (
          <div key={slot} className="grid gap-1.5 sm:grid-cols-[6rem_1fr] sm:items-start">
            <h3 className="text-sm font-medium sm:pt-2">{label}</h3>
            <div className="grid gap-2">
              {slotRows.map((row) => {
                const error = errors?.rows[row.key];
                const portionLabel = slot === "snack" ? `Portion of snack on day ${n} (${row.mealName})` : `Portion of ${lower} on day ${n}`;
                return (
                  <div key={row.key} className="grid gap-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <Link href={`/meals/${row.mealId}`} className="min-w-0 flex-1 truncate text-sm font-medium hover:underline">
                        {row.mealName}
                      </Link>
                      <Input
                        aria-label={portionLabel}
                        aria-invalid={error ? true : undefined}
                        aria-describedby={error ? `${row.key}-error` : undefined}
                        inputMode="decimal"
                        className="h-9 w-20 text-base md:text-sm"
                        value={row.portion}
                        onChange={(e) => onPortion(row.key, e.target.value)}
                      />
                      {slot === "snack" ? null : (
                        <Button type="button" variant="ghost" size="sm" aria-label={`Swap ${lower} on day ${n}`} onClick={() => onSwap(row)}>
                          Swap
                        </Button>
                      )}
                      <Button
                        type="button"
                        variant="ghost"
                        size="icon-sm"
                        aria-label={slot === "snack" ? `Remove ${row.mealName} from day ${n} snacks` : `Remove ${lower} from day ${n}`}
                        onClick={() => onRemove(row.key)}
                      >
                        <X aria-hidden />
                      </Button>
                    </div>
                    {error ? (
                      <p id={`${row.key}-error`} className="text-destructive text-sm">
                        {error}
                      </p>
                    ) : null}
                  </div>
                );
              })}
              {slot === "snack" || slotRows.length === 0 ? (
                <div>
                  <Button type="button" variant="outline" size="sm" aria-label={slot === "snack" ? `Add snack to day ${n}` : `Add ${lower} to day ${n}`} onClick={() => onAdd(slot)}>
                    <Plus aria-hidden />
                    {slot === "snack" ? "Add snack" : "Add meal"}
                  </Button>
                </div>
              ) : null}
            </div>
          </div>
        );
      })}
    </section>
  );
}
