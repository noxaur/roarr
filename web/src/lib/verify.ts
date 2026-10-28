import type { ActivityItem, VerifyCheck, VerifyRecord } from "./api";

// Verdicts from the download checks (internal/verify), reduced to what a
// person decides on: re-check, re-grab, or keep it anyway.

/**
 * What a record means for the person looking at it:
 *   fail     — failed a check, nobody kept it: decide
 *   warn     — passed with doubts: decide if it matters
 *   regrabFailed — thrown away, but the re-grab did not go through: decide
 *   checking — not judged yet
 *   ok       — passed, could not run, kept anyway, or re-grabbed
 */
export type Outcome = "fail" | "warn" | "regrabFailed" | "checking" | "ok";

export function outcome(r: VerifyRecord): Outcome {
  if (r.rejected) return r.regrab?.error ? "regrabFailed" : "ok";
  if (r.dismissed) return "ok";
  if (r.state !== "done" || !r.verdict) return "checking";
  if (r.verdict.level === "fail") return "fail";
  if (r.verdict.level === "warn") return "warn";
  return "ok";
}

/** Records that wait for a decision and belong with stuck downloads. */
export const isBlocking = (r: VerifyRecord) => {
  const o = outcome(r);
  return o === "fail" || o === "regrabFailed";
};

/** Records worth a badge: anything a person may want to act on. */
export const isFlagged = (r: VerifyRecord) => {
  const o = outcome(r);
  return o === "fail" || o === "warn" || o === "regrabFailed";
};

const RANK: Record<string, number> = { fail: 0, warn: 1, skip: 2, pass: 3 };

/** The checks that did not pass, worst first. Order within a level is kept. */
export function problems(r: VerifyRecord): VerifyCheck[] {
  const checks = r.verdict?.checks ?? [];
  return checks
    .map((c, i) => ({ c, i }))
    .filter(({ c }) => c.level === "fail" || c.level === "warn")
    .sort((a, b) => RANK[a.c.level] - RANK[b.c.level] || a.i - b.i)
    .map(({ c }) => c);
}

/** One line for a row: the reason a person reads first. Pair it with tag(). */
export function headline(r: VerifyRecord): string {
  if (outcome(r) === "regrabFailed") return r.regrab?.error ?? "";
  return r.verdict?.summary ?? "";
}

/** Short tag text for a badge. */
export function tag(r: VerifyRecord): string {
  switch (outcome(r)) {
    case "fail":
      return "fails check";
    case "warn":
      return "check warns";
    case "regrabFailed":
      return "re-grab failed";
    case "checking":
      return "checking";
  }
  return "checked";
}

const lower = (s?: string) => (s ?? "").toLowerCase();

/** Records by download id (lowercase). */
export function byHash(records: VerifyRecord[]): Map<string, VerifyRecord> {
  const m = new Map<string, VerifyRecord>();
  for (const r of records) m.set(lower(r.hash), r);
  return m;
}

/** The record for a download in the queue, if the verifier follows it. */
export function forActivity(index: Map<string, VerifyRecord>, a: ActivityItem): VerifyRecord | undefined {
  return a.downloadId ? index.get(lower(a.downloadId)) : undefined;
}

/**
 * The newest record that covers an episode. Records come newest first from
 * the API, but sort again so the answer does not depend on it.
 */
export function forEpisode(records: VerifyRecord[], seriesId: number, episodeId: number): VerifyRecord | undefined {
  return newest(records.filter((r) => r.seriesId === seriesId && r.episodeIds?.includes(episodeId)));
}

/** The newest record for a movie. */
export function forMovie(records: VerifyRecord[], movieId: number): VerifyRecord | undefined {
  return newest(records.filter((r) => r.kind === "movie" && r.movieId === movieId));
}

function newest(rs: VerifyRecord[]): VerifyRecord | undefined {
  let best: VerifyRecord | undefined;
  for (const r of rs) if (!best || Date.parse(r.added) > Date.parse(best.added)) best = r;
  return best;
}

/**
 * Blocking records whose download already left the queue: they still need a
 * decision, so the Downloads list shows them as their own rows.
 */
export function orphans(records: VerifyRecord[], activity: ActivityItem[]): VerifyRecord[] {
  const queued = new Set(activity.map((a) => lower(a.downloadId)).filter(Boolean));
  return records.filter((r) => isBlocking(r) && !queued.has(lower(r.hash)));
}

/** Link to the title a record belongs to. */
export function hrefOf(r: VerifyRecord): string | undefined {
  if (r.kind === "tv" && r.seriesId) return `/tv/${r.seriesId}`;
  if (r.kind === "movie" && r.movieId) return `/movie/${r.movieId}`;
  return undefined;
}
