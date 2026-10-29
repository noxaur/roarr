import { useEffect, useState } from "react";
import { api, type Kind, type ScheduleWindow, type TranscodeJob, type TranscodeStatus } from "./api";
import { pad } from "./format";

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

/** Short text for a file being converted: "AV1 42%", "AV1 paused", "AV1 queued". */
export function jobTag(j: TranscodeJob): string {
  if (j.state !== "running") return "AV1 queued";
  return j.paused ? "AV1 paused" : `AV1 ${Math.floor(j.percent)}%`;
}

const DAY_NAMES = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];

const LONG_NAMES = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"];

/** Days in the order a week is shown, Monday first. */
export const WEEK = [1, 2, 3, 4, 5, 6, 0].map((d) => ({ day: d, short: DAY_NAMES[d], name: LONG_NAMES[d] }));

/** A new schedule window: every night. */
export const newWindow = (): ScheduleWindow => ({ days: [1, 2, 3, 4, 5, 6, 0], from: "23:00", to: "07:00" });

/**
 * A moment as the server's wall clock shows it, "07:00", or "Tue 07:00" when
 * it is not today there. The schedule is in server time, so the browser's
 * own zone is not used.
 */
export function serverClock(iso: string, offsetSec: number, now = new Date()): string {
  const at = new Date(Date.parse(iso) + offsetSec * 1000);
  const here = new Date(now.getTime() + offsetSec * 1000);
  const hm = `${pad(at.getUTCHours())}:${pad(at.getUTCMinutes())}`;
  return at.toISOString().slice(0, 10) === here.toISOString().slice(0, 10) ? hm : `${DAY_NAMES[at.getUTCDay()]} ${hm}`;
}

/** What holds or limits the queue: "Paused", "Waiting until 23:00", "Running until 07:00"; "" when it runs freely. */
export function holdText(st: TranscodeStatus, now = new Date()): string {
  if (!st.enabled) return "";
  if (st.paused) return "Paused";
  const at = st.nextChange ? serverClock(st.nextChange, st.zoneOffset ?? 0, now) : "";
  if (st.held) return at ? `Waiting until ${at}` : "Waiting for schedule";
  return st.scheduled && at ? `Running until ${at}` : "";
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
