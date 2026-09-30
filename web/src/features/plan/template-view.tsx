import { servingsLabel } from "@/features/meals/meal-list";
import { CopyTemplateButton } from "./copy-template-button";
import { SLOTS, SLOT_LABELS } from "./plan-cache";
import type { DietTemplate } from "./template-queries";

/** A partner's template: every slot visible, nothing editable and nothing to apply. Copy it to change it or to use it. */
export function TemplateView({ template }: { template: DietTemplate }) {
  const days = Array.from({ length: template.day_count }, (_, dayIndex) =>
    template.slots.filter((slot) => slot.day_index === dayIndex).sort((a, b) => SLOTS.indexOf(a.slot) - SLOTS.indexOf(b.slot)),
  );

  return (
    <div className="grid gap-6">
      <div className="flex flex-wrap items-center gap-3">
        <p className="text-muted-foreground text-sm">Shared by your partner. Copy it to your library to change or apply it.</p>
        <CopyTemplateButton templateId={template.id} templateName={template.name} variant="default" />
      </div>
      <div className="grid gap-4 lg:grid-cols-2">
        {days.map((slots, dayIndex) => (
          <section key={dayIndex} aria-label={`Day ${dayIndex + 1}`} className="bg-card grid content-start gap-2 rounded-xl border p-4">
            <h2 className="font-semibold">Day {dayIndex + 1}</h2>
            {slots.length === 0 ? (
              <p className="text-muted-foreground text-sm">Nothing planned.</p>
            ) : (
              <ul className="divide-y text-sm">
                {slots.map((slot) => (
                  <li key={slot.id} className="flex items-baseline gap-3 py-1.5">
                    <span className="text-muted-foreground w-24 shrink-0">{SLOT_LABELS[slot.slot]}</span>
                    <span className="min-w-0 flex-1 truncate font-medium">{slot.meal_name}</span>
                    <span className="text-muted-foreground tabular-nums">{servingsLabel(slot.portion)}</span>
                  </li>
                ))}
              </ul>
            )}
          </section>
        ))}
      </div>
    </div>
  );
}
