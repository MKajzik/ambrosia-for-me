export type Parsed = { ok: true; value: number | null } | { ok: false };

const DECIMAL = /^(\d+(\.\d*)?|\.\d+)$/;

/**
 * Reads a number the way people type it: a comma is a decimal point, blank is "nothing entered".
 * Signs, exponents, hex and stray text are invalid, so nothing surprising reaches the API.
 */
export function parseDecimal(text: string): Parsed {
  const trimmed = text.trim().replace(",", ".");
  if (trimmed === "") return { ok: true, value: null };
  if (!DECIMAL.test(trimmed)) return { ok: false };
  const value = Number(trimmed);
  return Number.isFinite(value) ? { ok: true, value } : { ok: false };
}
