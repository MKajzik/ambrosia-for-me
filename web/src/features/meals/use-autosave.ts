import { useCallback, useEffect, useRef, useState } from "react";

type Options<T> = {
  /** The latest value worth saving, or null while the draft is not valid. Keep its identity stable between edits. */
  value: T | null;
  /** Whether `value` differs from what the server has. */
  dirty: boolean;
  save: (value: T) => Promise<void>;
  delayMs?: number;
};

/**
 * Saves `value` once it has stopped changing for `delayMs`. One save at a time; an edit made during a save is saved after it.
 * A value that has been tried (saved or failed) is not tried again by itself, so a failing server is not hammered:
 * the next edit, `retry()` or closing the editor tries again.
 */
export function useAutosave<T>({ value, dirty, save, delayMs = 700 }: Options<T>) {
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const inFlight = useRef(false);
  const settled = useRef<T | null>(null);
  const latest = useRef({ value, dirty, save });

  useEffect(() => {
    latest.current = { value, dirty, save };
  });

  const run = useCallback(async () => {
    const { value: current, dirty: isDirty, save: doSave } = latest.current;
    if (inFlight.current || !isDirty || current === null) return;
    inFlight.current = true;
    setSaving(true);
    setError(null);
    try {
      await doSave(current);
    } catch (caught) {
      setError(caught);
    } finally {
      settled.current = current;
      inFlight.current = false;
      setSaving(false);
    }
  }, []);

  const retry = useCallback(async () => {
    settled.current = null;
    await run();
  }, [run]);

  useEffect(() => {
    if (!dirty || value === null || saving || value === settled.current) return;
    const timer = setTimeout(() => void run(), delayMs);
    return () => clearTimeout(timer);
  }, [dirty, value, saving, delayMs, run]);

  // Closing the editor with an edit still waiting for its pause saves it instead of dropping it.
  useEffect(
    () => () => {
      void run();
    },
    [run],
  );

  return { saving, error, flush: run, retry };
}
