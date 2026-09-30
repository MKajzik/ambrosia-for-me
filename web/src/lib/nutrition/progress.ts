export type TargetProgress = {
  /** How much of the ring to fill, 0 to 1 (a value over target fills it). */
  fraction: number;
  /** The real ratio as a rounded percent, so 125 means 25% over. */
  percent: number;
  over: boolean;
};

/** How far `value` is towards `target`, or null when either is missing or the target is not a positive number. */
export function targetProgress(value: number | null, target: number | null): TargetProgress | null {
  if (value === null || target === null || Number.isNaN(value) || Number.isNaN(target) || !(target > 0) || value < 0) return null;
  const ratio = value / target;
  return { fraction: Math.min(ratio, 1), percent: Math.round(ratio * 100), over: ratio > 1 };
}
