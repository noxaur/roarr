import { useCallback, useMemo, useState } from "react";
import { toast } from "sonner";
import type { GrabResult, Release } from "./api";
import { errText } from "./actions";
import { planEpisodes } from "./plan";
import type { SearchState } from "./useSearch";

export type Kind = "tv" | "movie";

export interface GrabOpts {
  force?: boolean;
  bypass?: boolean;
  watchFirst?: boolean;
}

export interface PlanPick {
  release: Release;
  fills: number[];
}

export interface LivePlan {
  /** Releases that matter for the current selection. */
  all: Release[];
  picks: PlanPick[];
  missing: number[];
  size: number;
}

/**
 * Which releases roarr would grab for the wanted set, recomputed as each
 * indexer answers. TV uses the shared planner (packs vs. single episodes);
 * movies take the best-scored release per movie.
 */
export function useLivePlan(state: SearchState, relevant: (r: Release) => boolean, wanted: number[], kind: Kind): LivePlan {
  // Everything derived from the release map recomputes on new batches only.
  const all = useMemo(() => [...state.releases.values()].filter(relevant), [state.version, relevant]); // eslint-disable-line react-hooks/exhaustive-deps
  return useMemo(() => {
    let picks: PlanPick[];
    let missing: number[];
    if (kind === "movie") {
      const best = new Map<number, Release>();
      for (const r of all) {
        if (r.rejected || !r.movieTmdb) continue;
        const cur = best.get(r.movieTmdb);
        if (!cur || r.score > cur.score) best.set(r.movieTmdb, r);
      }
      picks = wanted.filter((id) => best.has(id)).map((id) => ({ release: best.get(id)!, fills: [id] }));
      missing = wanted.filter((id) => !best.has(id));
    } else {
      const byKey = new Map(all.map((r) => [r.key, r]));
      const pl = planEpisodes(
        wanted,
        all.map((r) => ({ key: r.key, score: r.score, rejected: r.rejected, covers: r.covers ?? [] })),
      );
      picks = pl.picks.map((x) => ({ release: byKey.get(x.key)!, fills: x.fills }));
      missing = pl.missing;
    }
    return { all, picks, missing, size: picks.reduce((n, x) => n + x.release.size, 0) };
  }, [all, wanted, kind]);
}

/** "Best" for this selection: a 60-episode pack is a poor answer to "just episode 5". */
export function fitScore(r: Release, wanted: Set<number>, kind: Kind): number {
  if (kind === "movie" || !r.covers?.length) return r.score;
  const fills = r.covers.filter((id) => wanted.has(id)).length;
  if (fills === 0) return -1;
  const extras = r.covers.length - fills;
  return (r.score * fills - 0.35 * r.score * extras - 8) / fills;
}

export interface Grabber {
  busy: Set<string>;
  results: Map<string, GrabResult>;
  grab: (r: Release, opts?: GrabOpts, quiet?: boolean) => Promise<GrabResult>;
  grabAll: (rs: Release[], opts?: GrabOpts) => Promise<number>;
}

/** Tracks in-flight grabs and their outcomes, shared by the card and the sheet. */
export function useGrabber(onGrab: (r: Release, opts: GrabOpts) => Promise<GrabResult>): Grabber {
  const [busy, setBusy] = useState<Set<string>>(new Set());
  const [results, setResults] = useState<Map<string, GrabResult>>(new Map());

  const grab = useCallback(
    async (r: Release, opts: GrabOpts = {}, quiet = false) => {
      setBusy((s) => new Set(s).add(r.key));
      try {
        const out = await onGrab(r, opts);
        setResults((m) => new Map(m).set(r.key, out));
        if (out.ok && !quiet) toast.success(out.message);
        else if (!out.canForce && !out.canBypass) toast.error(out.message, { description: out.rejections?.join(" · ") });
        return out;
      } catch (e) {
        const out: GrabResult = { ok: false, message: errText(e) };
        setResults((m) => new Map(m).set(r.key, out));
        toast.error(out.message);
        return out;
      } finally {
        setBusy((s) => {
          const n = new Set(s);
          n.delete(r.key);
          return n;
        });
      }
    },
    [onGrab],
  );

  // Several grabs report once, at the end; failures still toast on their own.
  const grabAll = useCallback(
    async (rs: Release[], opts: GrabOpts = {}) => {
      if (rs.length === 1) return (await grab(rs[0], opts)).ok ? 1 : 0;
      let ok = 0;
      for (const r of rs) {
        if (results.get(r.key)?.ok || (await grab(r, opts, true)).ok) ok++;
      }
      if (ok) toast.success(`Sent ${ok} of ${rs.length} releases to the download client`);
      return ok;
    },
    [grab, results],
  );

  return { busy, results, grab, grabAll };
}
