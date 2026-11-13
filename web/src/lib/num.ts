/**
 * What a number field's text means for settings: a finite number inside
 * [min, max], or null while the text is empty or half-typed ("-", "1e").
 */
export function parseNum(text: string, min: number, max?: number): number | null {
  if (!text.trim()) return null;
  const n = Number(text);
  if (!Number.isFinite(n)) return null;
  return Math.min(max ?? Infinity, Math.max(min, n));
}
