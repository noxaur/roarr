import { useEffect, useState } from "react";
import { api, type Kind, type TranscodeJob, type TranscodeStatus } from "./api";

// AV1 conversion state. Admin shows the queue; a title page only marks the
// files being converted right now.

export const isPending = (j: TranscodeJob) => j.state === "queued" || j.state === "running";

/** The last path segment, for either slash. */
export const baseName = (p: string) => p.slice(Math.max(p.lastIndexOf("/"), p.lastIndexOf("\\")) + 1);

/**
 * Pending jobs of one title, by file name. Paths differ between roarr and
 * the *arr when a path map is set, so the file name is the key.
 */
export function pendingFor(st: TranscodeStatus | null, kind: Kind, titleId: number): Map<string, TranscodeJob> {
  const m = new Map<string, TranscodeJob>();
  if (!st?.enabled) return m;
  for (const j of st.jobs ?? []) if (isPending(j) && j.kind === kind && j.titleId === titleId) m.set(baseName(j.path), j);
  return m;
}

/** Short text for a file being converted: "AV1 42%", "AV1 queued". */
export function jobTag(j: TranscodeJob): string {
  return j.state === "running" ? `AV1 ${Math.floor(j.percent)}%` : "AV1 queued";
}

/** Share of the source size saved, 0..1; 0 when unknown. */
export function savedShare(j: TranscodeJob): number {
  return j.srcBytes && j.savedBytes ? j.savedBytes / j.srcBytes : 0;
}

/**
 * Pending conversions for a title page. One request on open; it keeps
 * polling only while something of this title is queued or running.
 */
export function useConverting(kind: Kind, titleId: number): Map<string, TranscodeJob> {
  const [jobs, setJobs] = useState<Map<string, TranscodeJob>>(new Map());
  useEffect(() => {
    if (!titleId) return;
    let stop = false;
    let t: ReturnType<typeof setTimeout> | undefined;
    const tick = () =>
      api
        .transcode()
        .then((st) => {
          if (stop) return;
          const m = pendingFor(st, kind, titleId);
          setJobs(m);
          if (m.size > 0) t = setTimeout(tick, 5000);
        })
        .catch(() => {});
    tick();
    return () => {
      stop = true;
      clearTimeout(t);
    };
  }, [kind, titleId]);
  return jobs;
}
