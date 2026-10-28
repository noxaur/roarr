import type { VerifySettings } from "./api";

// Pure helpers for the Admin page. PUT /api/settings replaces the whole
// object, so the page edits a full copy of what GET returned and these
// helpers change one field without dropping any other.

export type VerifyMode = "auto" | "on" | "off";

/** verify.enabled is a tri-state: unset means "on when ffprobe is found". */
export function verifyMode(v: VerifySettings | undefined): VerifyMode {
  if (v?.enabled == null) return "auto";
  return v.enabled ? "on" : "off";
}

/** Sets the tri-state; "auto" removes the key so Go decodes it as nil. */
export function setVerifyMode(v: VerifySettings, mode: VerifyMode): void {
  if (mode === "auto") delete v.enabled;
  else v.enabled = mode === "on";
}

/**
 * Entries of a "from=to,from=to" path map that the server ignores: no "=",
 * or an empty side.
 */
export function badPathPairs(s: string | undefined): string[] {
  return (s ?? "")
    .split(",")
    .map((p) => p.trim())
    .filter(Boolean)
    .filter((p) => {
      const i = p.indexOf("=");
      return i < 0 || !p.slice(0, i).trim() || !p.slice(i + 1).trim();
    });
}

/** Number of leaf values that differ, for the save bar. */
export function countChanges(a: unknown, b: unknown): number {
  if (Array.isArray(a) || Array.isArray(b)) return JSON.stringify(a ?? []) === JSON.stringify(b ?? []) ? 0 : 1;
  if (a && b && typeof a === "object" && typeof b === "object") {
    const keys = new Set([...Object.keys(a), ...Object.keys(b)]);
    let n = 0;
    for (const k of keys) n += countChanges((a as Record<string, unknown>)[k], (b as Record<string, unknown>)[k]);
    return n;
  }
  return (a ?? "") === (b ?? "") ? 0 : 1;
}
