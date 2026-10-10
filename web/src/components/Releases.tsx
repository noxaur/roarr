import { useMemo, useState } from "react";
import type { GrabResult, Release } from "../lib/api";
import { planEpisodes } from "../lib/plan";
import type { SearchState } from "../lib/useSearch";
import { age, bytes, cls, epCode } from "../lib/format";
import { BoltIcon, CheckIcon, DownloadIcon, ExternalIcon } from "./Icons";
import { useToast } from "./Toasts";

export interface GrabOpts {
  force?: boolean;
  watchFirst?: boolean;
}

type Sort = "best" | "seeders" | "size" | "newest";

interface Props {
  state: SearchState;
  /** Which releases matter for the current scope. */
  relevant: (r: Release) => boolean;
  /** Episode IDs (tv) or movie TMDB ids (movies) the user wants. */
  wanted: number[];
  kind: "tv" | "movie";
  scopeLabel: string;
  /** Map an episode id to a label (S01E02) for plan summaries. */
  epLabel?: (id: number) => string;
  movieLabel?: (tmdb: number) => string;
  onGrab: (r: Release, opts: GrabOpts) => Promise<GrabResult>;
  watchFirstDefault: boolean;
  empty?: string;
}

export function Releases(p: Props) {
  const { state } = p;
  const toast = useToast();
  const [sort, setSort] = useState<Sort>("best");
  const [res, setRes] = useState<number | null>(null);
  const [shape, setShape] = useState<"all" | "packs" | "singles">("all");
  const [showRejected, setShowRejected] = useState(false);
  const [open, setOpen] = useState<string | null>(null);
  const [limit, setLimit] = useState(60);
  const [grabbing, setGrabbing] = useState<Set<string>>(new Set());
  const [results, setResults] = useState<Map<string, GrabResult>>(new Map());
  const [watchFirst, setWatchFirst] = useState(p.watchFirstDefault);

  // Everything derived from the release map recomputes on new batches only.
  const all = useMemo(() => [...state.releases.values()].filter(p.relevant), [state.version, p.relevant]); // eslint-disable-line react-hooks/exhaustive-deps
  const resolutions = useMemo(() => {
    const s = new Set<number>();
    for (const r of all) if (r.info.resolution && !r.rejected) s.add(r.info.resolution);
    return [...s].sort((a, b) => b - a);
  }, [all]);

  const rejectedCount = all.filter((r) => r.rejected).length;
  const list = useMemo(() => {
    let xs = all.filter((r) => (showRejected || !r.rejected) && (!res || r.info.resolution === res));
    if (p.kind === "tv" && shape !== "all") xs = xs.filter((r) => ((r.covers?.length ?? 0) > 1) === (shape === "packs"));
    // "Best" means best for this selection: a 60-episode pack is a poor
    // answer to "just episode 5", so rank by the planner's per-episode value.
    const want = new Set(p.wanted);
    const fit = (r: Release) => {
      if (p.kind === "movie" || !r.covers?.length) return r.score;
      const fills = r.covers.filter((id) => want.has(id)).length;
      if (fills === 0) return -1;
      const extras = r.covers.length - fills;
      return (r.score * fills - 0.35 * r.score * extras - 8) / fills;
    };
    const by: Record<Sort, (a: Release, b: Release) => number> = {
      best: (a, b) => Number(a.rejected) - Number(b.rejected) || fit(b) - fit(a) || b.seeders - a.seeders,
      seeders: (a, b) => b.seeders - a.seeders,
      size: (a, b) => b.size - a.size,
      newest: (a, b) => a.ageHours - b.ageHours,
    };
    return xs.sort(by[sort]);
  }, [all, showRejected, res, shape, sort, p.kind, p.wanted]);

  // Live plan: which releases roarr would grab for the wanted set.
  const plan = useMemo(() => {
    if (p.kind === "movie") {
      const best = new Map<number, Release>();
      for (const r of all) {
        if (r.rejected || !r.movieTmdb) continue;
        const cur = best.get(r.movieTmdb);
        if (!cur || r.score > cur.score) best.set(r.movieTmdb, r);
      }
      const picks = p.wanted.filter((id) => best.has(id)).map((id) => ({ release: best.get(id)!, fills: [id] }));
      return { picks, missing: p.wanted.filter((id) => !best.has(id)) };
    }
    const byKey = new Map(all.map((r) => [r.key, r]));
    const pl = planEpisodes(
      p.wanted,
      all.map((r) => ({ key: r.key, score: r.score, rejected: r.rejected, covers: r.covers ?? [] })),
    );
    return { picks: pl.picks.map((x) => ({ release: byKey.get(x.key)!, fills: x.fills })), missing: pl.missing };
  }, [all, p.wanted, p.kind]);
  const planKeys = new Set(plan.picks.map((x) => x.release.key));
  const planSize = plan.picks.reduce((n, x) => n + x.release.size, 0);

  const grab = async (r: Release, opts: GrabOpts = {}) => {
    setGrabbing((s) => new Set(s).add(r.key));
    try {
      const out = await p.onGrab(r, { watchFirst, ...opts });
      setResults((m) => new Map(m).set(r.key, out));
      if (out.ok) toast(<span>{out.message}</span>, "ok");
      return out;
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      setResults((m) => new Map(m).set(r.key, { ok: false, message: msg }));
      toast(msg, "bad");
      return { ok: false, message: msg } as GrabResult;
    } finally {
      setGrabbing((s) => {
        const n = new Set(s);
        n.delete(r.key);
        return n;
      });
    }
  };

  const grabPlan = async () => {
    for (const pick of plan.picks) {
      if (results.get(pick.release.key)?.ok) continue;
      await grab(pick.release);
    }
  };

  const indexers = [...state.indexers.values()].sort((a, b) => a.name.localeCompare(b.name));
  const pendingIx = indexers.filter((i) => !i.disabled && i.answered < i.queries);
  const elapsed = (t?: number) => (t ? `${((t - state.startedAt) / 1000).toFixed(1)}s` : "");
  const allCached = indexers.length > 0 && indexers.every((i) => i.cached || i.disabled);

  return (
    <div className="panel">
      <div className="scopebar">
        <div className="grow">
          <div style={{ fontWeight: 600 }}>{p.scopeLabel}</div>
          <div className="timing">
            {state.firstAt ? (
              <>
                first results <b>{elapsed(state.firstAt)}</b>
              </>
            ) : state.done ? (
              "no results"
            ) : (
              "searching…"
            )}
            {state.done && state.doneAt && <> · all indexers {elapsed(state.doneAt)}</>}
            {" · "}
            {all.length} releases{allCached && state.done ? " · from cache" : ""}
          </div>
        </div>
        {!state.done && <span className="spinner" />}
      </div>

      {indexers.length > 0 && (
        <div className="indexers">
          {indexers.map((i) => {
            const done = i.answered >= i.queries;
            const failed = i.errors.length > 0 && i.errors.length >= i.answered && done;
            return (
              <span key={i.id} className="ix" title={i.disabled ?? i.errors.join("\n")}>
                {i.disabled ? <span className="dot" /> : !done ? <span className="spinner" style={{ width: 9, height: 9, borderWidth: 1.5 }} /> : <span className={cls("dot", failed ? "bad" : i.errors.length ? "warn" : "ok")} />}
                {i.name}
                {done && !i.disabled && <span className="mono">{failed ? "failed" : i.cached ? `${i.results}·cache` : `${i.results}·${(i.ms / 1000).toFixed(1)}s`}</span>}
                {i.disabled && <span className="mono">off</span>}
              </span>
            );
          })}
        </div>
      )}

      {state.error && <div className="notice bad" style={{ margin: 14 }}>{state.error}</div>}

      {p.wanted.length > 0 && (plan.picks.length > 0 || state.done) && (
        <div className="plan">
          <div className="row">
            <BoltIcon style={{ color: "var(--accent)" }} />
            <b>Best pick</b>
            <span className="muted small">
              {plan.picks.length === 0
                ? "nothing suitable yet"
                : `${plan.picks.length} release${plan.picks.length > 1 ? "s" : ""} · ${bytes(planSize)}${pendingIx.length ? ` · ${pendingIx.length} indexer${pendingIx.length > 1 ? "s" : ""} still answering` : ""}`}
            </span>
            <span className="spacer" />
            {plan.picks.length > 0 && (
              <button className="btn primary sm" disabled={plan.picks.every((x) => results.get(x.release.key)?.ok) || grabbing.size > 0} onClick={grabPlan}>
                <DownloadIcon /> Get {plan.picks.length > 1 ? "all" : "it"}
              </button>
            )}
          </div>
          {plan.picks.slice(0, 6).map((x) => (
            <div key={x.release.key} className="plan-item">
              {results.get(x.release.key)?.ok ? <CheckIcon style={{ color: "var(--accent)" }} /> : <span className="dot ok" />}
              <span className="tag good">{coverLabel(x.release, x.fills, p)}</span>
              <span className="grow ellipsis" title={x.release.title}>
                {x.release.title}
              </span>
              <span className="mono small muted">{bytes(x.release.size)}</span>
            </div>
          ))}
          {plan.picks.length > 6 && <div className="small muted">+ {plan.picks.length - 6} more</div>}
          {plan.missing.length > 0 && state.done && (
            <div className="small" style={{ color: "var(--warn)", marginTop: 4 }}>
              Nothing suitable for {plan.missing.length} {p.kind === "tv" ? "episode" : "movie"}
              {plan.missing.length > 1 ? "s" : ""}
              {p.epLabel && plan.missing.length <= 6 ? `: ${plan.missing.map(p.epLabel).join(", ")}` : ""}
            </div>
          )}
        </div>
      )}

      <div className="filters">
        <div className="seg">
          {(["best", "seeders", "size", "newest"] as Sort[]).map((s) => (
            <button key={s} className={cls(sort === s && "on")} onClick={() => setSort(s)}>
              {{ best: "Best", seeders: "Seeders", size: "Size", newest: "Newest" }[s]}
            </button>
          ))}
        </div>
        {resolutions.map((r) => (
          <button key={r} className={cls("chip", res === r && "on")} onClick={() => setRes(res === r ? null : r)}>
            {r === 2160 ? "4K" : `${r}p`}
          </button>
        ))}
        {p.kind === "tv" && (
          <div className="seg">
            {(["all", "packs", "singles"] as const).map((s) => (
              <button key={s} className={cls(shape === s && "on")} onClick={() => setShape(s)}>
                {{ all: "All", packs: "Packs", singles: "Singles" }[s]}
              </button>
            ))}
          </div>
        )}
        <span className="spacer" />
        {p.kind === "tv" && (
          <label className="check small" title="Download a pack episode by episode, importing each one as soon as it finishes">
            <input type="checkbox" checked={watchFirst} onChange={(e) => setWatchFirst(e.target.checked)} />
            Watch-first
          </label>
        )}
        {rejectedCount > 0 && (
          <button className={cls("chip", showRejected && "on")} onClick={() => setShowRejected(!showRejected)}>
            {rejectedCount} filtered out
          </button>
        )}
      </div>

      <div className="rel-list">
        {list.length === 0 && (
          <div className="empty" style={{ margin: 14 }}>
            {state.done ? (p.empty ?? "No releases for this selection.") : "Waiting for indexers…"}
          </div>
        )}
        {list.slice(0, limit).map((r) => (
          <ReleaseRow
            key={r.key}
            r={r}
            p={p}
            picked={planKeys.has(r.key)}
            open={open === r.key}
            onToggle={() => setOpen(open === r.key ? null : r.key)}
            busy={grabbing.has(r.key)}
            result={results.get(r.key)}
            onGrab={(opts) => grab(r, opts)}
          />
        ))}
        {list.length > limit && (
          <div style={{ padding: 12, textAlign: "center" }}>
            <button className="btn sm" onClick={() => setLimit(limit + 100)}>
              Show {Math.min(100, list.length - limit)} more
            </button>
          </div>
        )}
      </div>
    </div>
  );
}

function coverLabel(r: Release, fills: number[], p: Props): string {
  if (p.kind === "movie") return p.movieLabel ? p.movieLabel(fills[0]) : "movie";
  const n = r.covers?.length ?? 0;
  if (r.kind === "complete") return `complete · ${n} eps`;
  if (r.kind === "multi-season") return `S${r.info.seasons?.[0]}–${r.info.seasons?.[r.info.seasons.length - 1]} · ${n} eps`;
  if (r.kind === "season") return `season ${r.info.seasons?.[0]} · ${n} eps`;
  if (fills.length === 1 && p.epLabel) return p.epLabel(fills[0]);
  return `${fills.length} eps`;
}

function kindTag(r: Release) {
  const n = r.covers?.length ?? 0;
  switch (r.kind) {
    case "complete":
      return <span className="tag info">Complete{n ? ` · ${n}` : ""}</span>;
    case "multi-season":
      return (
        <span className="tag info">
          S{r.info.seasons?.[0]}–S{r.info.seasons?.[r.info.seasons.length - 1]}
          {n ? ` · ${n}` : ""}
        </span>
      );
    case "season":
      return <span className="tag info">Season {r.info.seasons?.[0]}{n ? ` · ${n}` : ""}</span>;
    case "multi":
      return <span className="tag info">{n || r.info.episodes?.length} eps</span>;
    case "episode":
      if (r.info.seasons?.length && r.info.episodes?.length) return <span className="tag">{epCode(r.info.seasons[0], r.info.episodes[0])}</span>;
      if (r.info.absolute?.length) return <span className="tag">#{r.info.absolute[0]}</span>;
  }
  return null;
}

function ReleaseRow({
  r,
  p,
  picked,
  open,
  onToggle,
  busy,
  result,
  onGrab,
}: {
  r: Release;
  p: Props;
  picked: boolean;
  open: boolean;
  onToggle: () => void;
  busy: boolean;
  result?: GrabResult;
  onGrab: (o: GrabOpts) => void;
}) {
  const i = r.info;
  const scoreCls = r.score >= 75 ? "hi" : r.score >= 50 ? "mid" : "lo";
  const wantedSet = new Set(p.wanted);
  const fills = (r.covers ?? []).filter((id) => wantedSet.has(id)).length;
  const extras = (r.covers?.length ?? 0) - fills;
  return (
    <div className={cls("rel", r.rejected && "rejected", picked && "picked")}>
      <div className={cls("score", scoreCls)} title={(r.notes ?? []).join("\n")}>
        {Math.round(r.score)}
      </div>
      <div style={{ minWidth: 0, cursor: "pointer" }} onClick={onToggle}>
        <div className="rel-title">{r.title}</div>
        <div className="rel-meta">
          {p.kind === "tv" && kindTag(r)}
          {p.kind === "movie" && p.movieLabel && r.movieTmdb && p.wanted.length > 1 && <span className="tag">{p.movieLabel(r.movieTmdb)}</span>}
          {i.resolution ? <span className="tag res">{i.resolution === 2160 ? "4K" : `${i.resolution}p`}</span> : null}
          {i.source && <span className="tag">{i.source}</span>}
          {i.codec && <span className="tag">{i.codec}</span>}
          {i.hdr && <span className="tag warn">{i.hdr}</span>}
          {i.audio && <span className="tag">{i.audio}{i.channels ? ` ${i.channels}` : ""}</span>}
          {(i.dualAudio || i.multi) && <span className="tag good">{i.dualAudio ? "dual audio" : "multi"}</span>}
          {i.languages?.map((l) => (
            <span key={l} className="tag">{l}</span>
          ))}
          {(i.proper || i.repack) && <span className="tag good">{i.repack ? "repack" : "proper"}</span>}
          {i.group && <span className="tag">{i.group}</span>}
          <span className="tiny faint">
            {r.indexers.length > 1 ? `${r.indexers.length} indexers` : r.indexer}
          </span>
        </div>
        {r.rejected && r.reasons && <div className="why bad">{r.reasons.join(" · ")}</div>}
      </div>
      <div className="row">
        <div className="rel-stats">
          <span>{bytes(r.size)}</span>
          <span title="seeders" style={{ color: r.seeders >= 20 ? "var(--accent)" : r.seeders >= 5 ? "var(--text-2)" : "var(--warn)" }}>
            ▲{r.seeders}
          </span>
          <span className="muted">{age(r.ageHours)}</span>
        </div>
        {result?.ok ? (
          <span className="btn sm" style={{ color: "var(--accent)", borderColor: "transparent" }}>
            <CheckIcon />
          </span>
        ) : (
          <button className={cls("btn sm", picked && "primary")} disabled={busy} onClick={() => (open ? onGrab({}) : onToggle())}>
            {busy ? <span className="spinner" /> : <DownloadIcon />}
          </button>
        )}
      </div>
      {open && !result?.ok && (
        <div className="confirm">
          <span className="small">
            {p.kind === "tv" ? (
              <>
                Fills <b>{fills}</b> wanted episode{fills === 1 ? "" : "s"}
                {extras > 0 && <span className="muted"> (+{extras} you already have or did not pick)</span>}
              </>
            ) : (
              <>Movie release</>
            )}
            {" · "}
            {bytes(r.size)} · {r.seeders} seeders
          </span>
          <span className="spacer" />
          {r.infoUrl && (
            <a className="btn ghost sm" href={r.infoUrl} target="_blank" rel="noreferrer noopener">
              <ExternalIcon /> Details
            </a>
          )}
          {result && !result.ok && result.canForce && (
            <button className="btn sm danger" disabled={busy} onClick={() => onGrab({ force: true })}>
              Download anyway
            </button>
          )}
          <button className="btn primary sm" disabled={busy} onClick={() => onGrab({})}>
            {busy ? <span className="spinner" /> : <DownloadIcon />} Grab
          </button>
          {result && !result.ok && (
            <div className="why bad" style={{ flexBasis: "100%" }}>
              {result.message}
              {result.rejections?.length ? `: ${result.rejections.join(" · ")}` : ""}
            </div>
          )}
        </div>
      )}
    </div>
  );
}
