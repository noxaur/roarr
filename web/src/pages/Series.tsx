import { useCallback, useEffect, useMemo, useState } from "react";
import { api, type EpisodeDetail, type Release, type SeriesDetail } from "../lib/api";
import { useSearch } from "../lib/useSearch";
import { useRouter } from "../lib/router";
import { useStore } from "../lib/store";
import { Releases, type GrabOpts } from "../components/Releases";
import { PosterImg } from "../components/Poster";
import { useToast } from "../components/Toasts";
import { BoltIcon, ChevronIcon, CheckIcon, DownloadIcon } from "../components/Icons";
import { bytes, cls, epCode, pad } from "../lib/format";

export function SeriesPage({ id, tvdb, tmdb }: { id?: number; tvdb?: number; tmdb?: number }) {
  const [d, setD] = useState<SeriesDetail | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [prefs, setPrefs] = useState({ prefetch: true, watchFirst: true });

  const load = useCallback(
    (fresh = false) =>
      (id ? api.series(id, fresh) : api.seriesByTvdb(tvdb ?? 0, tmdb))
        .then((x) => setD({ ...x, seasons: (x.seasons ?? []).map((s) => ({ ...s, episodes: s.episodes ?? [] })) }))
        .catch((e) => setError(String(e.message ?? e))),
    [id, tvdb, tmdb],
  );
  useEffect(() => {
    load();
    api
      .settings()
      .then((s) => setPrefs({ prefetch: s.settings.behaviour.prefetch, watchFirst: s.settings.behaviour.watchFirst }))
      .catch(() => {});
  }, [load]);

  if (error) return <div className="notice bad">{error}</div>;
  if (!d) return <TitleSkeleton />;
  return <SeriesView d={d} reload={() => load(true)} prefs={prefs} />;
}

export function TitleSkeleton() {
  return (
    <div className="row" style={{ gap: 22, alignItems: "flex-start" }}>
      <div className="skeleton" style={{ width: 132, aspectRatio: "2/3" }} />
      <div className="grow">
        <div className="skeleton" style={{ height: 28, width: "40%", marginBottom: 12 }} />
        <div className="skeleton" style={{ height: 14, width: "70%", marginBottom: 8 }} />
        <div className="skeleton" style={{ height: 14, width: "60%" }} />
      </div>
    </div>
  );
}

function SeriesView({ d, reload, prefs }: { d: SeriesDetail; reload: () => void; prefs: { prefetch: boolean; watchFirst: boolean } }) {
  const { mode, setMode, reloadActivity, reloadLibrary } = useStore();
  const { navigate } = useRouter();
  const toast = useToast();
  const seasons = useMemo(() => d.seasons.filter((s) => s.episodes.length > 0 || d.synthetic), [d]);
  const allEps = useMemo(() => seasons.flatMap((s) => s.episodes), [seasons]);
  const byId = useMemo(() => new Map(allEps.map((e) => [e.id, e])), [allEps]);
  const missing = useMemo(() => allEps.filter((e) => e.season > 0 && e.aired && !e.hasFile && !e.queued).map((e) => e.id), [allEps]);
  const everything = useMemo(() => allEps.filter((e) => e.season > 0 && e.aired).map((e) => e.id), [allEps]);

  const [sel, setSel] = useState<Set<number>>(() => new Set(missing.length ? missing : everything));
  const [openSeasons, setOpenSeasons] = useState<Set<number>>(() => {
    const first = seasons.find((s) => s.episodes.some((e) => missing.includes(e.id)));
    return new Set(first ? [first.number] : seasons.length === 1 ? [seasons[0].number] : []);
  });
  const [autoBusy, setAutoBusy] = useState(false);

  const selArr = useMemo(() => [...sel].sort((a, b) => a - b), [sel]);
  const ref: Record<string, string> = d.inLibrary ? { id: String(d.id) } : { tvdb: String(d.tvdbId ?? 0), tmdb: String(d.tmdbId ?? 0) };

  // Streams: the whole show (prefetched as soon as the page opens) plus
  // exact episode queries when only a few episodes are selected.
  const streams = useMemo(() => {
    const out: Record<string, string>[] = [];
    if (prefs.prefetch || sel.size > 3) out.push({ kind: "tv", ...ref, show: "1" });
    if (sel.size > 0 && sel.size <= 3) {
      out.push({ kind: "tv", ...ref, episodes: selArr.filter((x) => x > 0).join(",") });
      if (!prefs.prefetch) {
        const ss = [...new Set(selArr.map((x) => byId.get(x)?.season).filter((x) => x !== undefined))];
        out.push({ kind: "tv", ...ref, seasons: ss.join(",") });
      }
    }
    return out.filter((s) => s.episodes !== "");
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selArr.join(","), prefs.prefetch, d.key]);
  const search = useSearch(d.key, streams);

  const relevant = useCallback(
    (r: Release) => {
      if (!r.covers || r.covers.length === 0) return d.synthetic === true && r.match;
      return r.covers.some((id) => sel.has(id));
    },
    [sel, d.synthetic],
  );

  const scopeLabel = useMemo(() => describeSelection(selArr, byId, seasons, missing, everything), [selArr, byId, seasons, missing, everything]);
  const epLabel = (id: number) => {
    const e = byId.get(id);
    return e ? epCode(e.season, e.number) : `#${id}`;
  };

  const onGrab = async (r: Release, opts: GrabOpts) => {
    const fills = (r.covers ?? []).filter((x) => sel.has(x));
    const res = await api.grab({
      kind: "tv",
      seriesId: d.inLibrary ? d.id : 0,
      tvdbId: d.tvdbId,
      episodeIds: fills.length ? fills : selArr,
      release: r,
      force: opts.force,
      watchFirst: opts.watchFirst,
    });
    if (res.ok) afterGrab(res.seriesId);
    return res;
  };

  const afterGrab = (seriesId?: number) => {
    reloadActivity();
    setTimeout(() => {
      if (!d.inLibrary && seriesId) {
        reloadLibrary();
        navigate(`/tv/${seriesId}`, { replace: true });
      } else reload();
    }, 1200);
  };

  const runAuto = async (episodeIds: number[], label: string) => {
    if (episodeIds.length === 0) return;
    setAutoBusy(true);
    toast(`Finding the best release for ${label}…`);
    try {
      const res = await api.auto({ kind: "tv", seriesId: d.inLibrary ? d.id : 0, tvdbId: d.tvdbId, tmdbId: d.tmdbId, episodeIds });
      const ok = (res.grabs ?? []).filter((g) => g.result.ok);
      toast(
        <div>
          <b>{res.message}</b>
          {(res.grabs ?? []).map((g) => (
            <div key={g.release} className="small muted ellipsis">
              {g.result.ok ? "✓" : "✕"} {g.release}
              {!g.result.ok && (g.error || g.result.rejections?.join(", "))}
            </div>
          ))}
          <div className="tiny faint">decided after {(res.waitedMs / 1000).toFixed(1)}s</div>
        </div>,
        ok.length ? "ok" : "bad",
      );
      if (ok.length) afterGrab(ok[0].result.seriesId);
    } catch (e) {
      toast(e instanceof Error ? e.message : String(e), "bad");
    } finally {
      setAutoBusy(false);
    }
  };

  const seasonWanted = (n: number) => {
    const s = seasons.find((x) => x.number === n);
    if (!s) return [];
    const aired = s.episodes.filter((e) => e.aired);
    const miss = aired.filter((e) => !e.hasFile && !e.queued);
    return (miss.length ? miss : aired).map((e) => e.id);
  };

  const pickSeason = (n: number) => {
    const ids = seasonWanted(n);
    if (mode === "auto") return runAuto(ids, `season ${n}`);
    setSel(new Set(ids));
  };
  const pickEpisode = (e: EpisodeDetail, additive: boolean) => {
    if (!additive) {
      if (mode === "auto") return runAuto([e.id], epCode(e.season, e.number));
      setSel(new Set([e.id]));
      return;
    }
    const n = new Set(sel);
    if (n.has(e.id)) n.delete(e.id);
    else n.add(e.id);
    setSel(n);
  };

  const have = allEps.filter((e) => e.season > 0 && e.hasFile).length;
  const total = allEps.filter((e) => e.season > 0 && e.aired).length;

  return (
    <>
      <div className="hero">
        {d.fanart && <div className="backdrop" style={{ backgroundImage: `url(${d.fanart})` }} />}
        <div className="hero-inner">
          <div className="poster">
            <PosterImg src={d.poster} title={d.title} />
          </div>
          <div className="grow">
            <div className="small muted">
              {[d.year, d.network, d.status, d.anime ? "anime" : null, d.runtime ? `${d.runtime} min` : null].filter(Boolean).join(" · ")}
            </div>
            <h1>{d.title}</h1>
            {d.overview && <p className="overview">{d.overview}</p>}
            <div className="row wrap" style={{ marginTop: 12 }}>
              {d.inLibrary ? (
                <span className="tag good">
                  {have}/{total} episodes{d.sizeOnDisk ? ` · ${bytes(d.sizeOnDisk)}` : ""}
                </span>
              ) : (
                <span className="tag info">Not in Sonarr yet. Added automatically when you grab.</span>
              )}
              <span className="spacer" />
              <div className="seg" title="Manual: you pick the release. Auto: roarr picks and grabs the best one.">
                <button className={cls(mode === "manual" && "on")} onClick={() => setMode("manual")}>
                  Manual
                </button>
                <button className={cls(mode === "auto" && "on")} onClick={() => setMode("auto")}>
                  <BoltIcon width={12} height={12} /> Auto
                </button>
              </div>
              <button className="btn sm" disabled={missing.length === 0 || autoBusy} onClick={() => (mode === "auto" ? runAuto(missing, "everything missing") : setSel(new Set(missing)))}>
                Missing · {missing.length}
              </button>
              <button className="btn sm" disabled={autoBusy} onClick={() => (mode === "auto" ? runAuto(everything, "the whole show") : setSel(new Set(everything)))}>
                Whole show
              </button>
            </div>
          </div>
        </div>
      </div>

      <div className="title-layout">
        <div className="seasons">
          {seasons.map((s) => {
            const aired = s.episodes.filter((e) => e.aired);
            const haveS = aired.filter((e) => e.hasFile).length;
            const isOpen = openSeasons.has(s.number);
            const selected = s.episodes.length > 0 && s.episodes.filter((e) => e.aired).every((e) => sel.has(e.id));
            return (
              <div key={s.number} className={cls("season", selected && "selected")}>
                <div
                  className="season-head"
                  onClick={() => {
                    const n = new Set(openSeasons);
                    if (n.has(s.number)) n.delete(s.number);
                    else n.add(s.number);
                    setOpenSeasons(n);
                  }}
                >
                  <ChevronIcon className={cls("caret", isOpen && "open")} />
                  <b>{s.number === 0 ? "Specials" : `Season ${s.number}`}</b>
                  <span className="small muted mono">
                    {d.synthetic ? `${s.episodes.length} eps` : `${haveS}/${aired.length}`}
                  </span>
                  {!d.synthetic && aired.length > 0 && (
                    <div className="progress-mini">
                      <i style={{ width: `${(haveS / aired.length) * 100}%` }} />
                    </div>
                  )}
                  <span className="spacer" />
                  {s.episodes.length > 0 && (
                    <button
                      className={cls("btn xs", mode === "auto" && "primary")}
                      disabled={autoBusy}
                      onClick={(e) => {
                        e.stopPropagation();
                        pickSeason(s.number);
                      }}
                    >
                      {mode === "auto" ? <BoltIcon width={11} height={11} /> : null}
                      {mode === "auto" ? "Get" : "Select"}
                    </button>
                  )}
                </div>
                {isOpen && s.episodes.length > 0 && (
                  <div className="episodes">
                    {s.episodes.map((e) => (
                      <div
                        key={e.id}
                        className={cls("episode", sel.has(e.id) && "sel", !e.aired && "unaired")}
                        onClick={(ev) => pickEpisode(e, ev.shiftKey || ev.metaKey || ev.ctrlKey)}
                      >
                        <input
                          type="checkbox"
                          checked={sel.has(e.id)}
                          onClick={(ev) => ev.stopPropagation()}
                          onChange={() => pickEpisode(e, true)}
                          style={{ accentColor: "var(--accent)" }}
                        />
                        <span className="num">
                          {e.absolute && d.anime ? `#${e.absolute}` : `E${pad(e.number)}`}
                        </span>
                        <span className="grow ellipsis">{e.title}</span>
                        {e.hasFile ? (
                          <CheckIcon style={{ color: "var(--accent)" }} />
                        ) : e.queued ? (
                          <span className="dot info pulse" title="downloading" />
                        ) : e.aired ? (
                          <span className="dot warn" title="missing" />
                        ) : (
                          <span className="tiny faint">{e.airDate ?? "TBA"}</span>
                        )}
                        {e.aired && mode === "auto" && (
                          <button
                            className="btn xs quick"
                            disabled={autoBusy}
                            title="Grab the best release for this episode"
                            onClick={(ev) => {
                              ev.stopPropagation();
                              runAuto([e.id], epCode(e.season, e.number));
                            }}
                          >
                            <DownloadIcon width={11} height={11} />
                          </button>
                        )}
                      </div>
                    ))}
                  </div>
                )}
              </div>
            );
          })}
          <div className="small faint" style={{ padding: "4px 2px" }}>
            Click an episode to look at just that one; shift-click to pick several.
          </div>
        </div>

        <div className="sticky">
          <Releases
            state={search}
            relevant={relevant}
            wanted={selArr}
            kind="tv"
            scopeLabel={scopeLabel}
            epLabel={epLabel}
            onGrab={onGrab}
            watchFirstDefault={prefs.watchFirst}
            empty={sel.size === 0 ? "Pick episodes or a season on the left." : undefined}
          />
        </div>
      </div>
    </>
  );
}

function describeSelection(
  sel: number[],
  byId: Map<number, EpisodeDetail>,
  seasons: SeriesDetail["seasons"],
  missing: number[],
  everything: number[],
): string {
  if (sel.length === 0) return "Nothing selected";
  const same = (a: number[], b: number[]) => a.length === b.length && a.every((x, i) => x === b[i]);
  const sortedMissing = [...missing].sort((a, b) => a - b);
  const sortedAll = [...everything].sort((a, b) => a - b);
  if (same(sel, sortedAll)) return `Whole show · ${sel.length} episodes`;
  if (same(sel, sortedMissing)) return `Everything missing · ${sel.length} episode${sel.length > 1 ? "s" : ""}`;
  if (sel.length === 1) {
    const e = byId.get(sel[0]);
    return e ? `${epCode(e.season, e.number)} · ${e.title}` : "1 episode";
  }
  const ss = new Set(sel.map((id) => byId.get(id)?.season));
  if (ss.size === 1) {
    const n = [...ss][0]!;
    const season = seasons.find((s) => s.number === n);
    const full = season && season.episodes.filter((e) => e.aired).length === sel.length;
    return full ? `Season ${n}` : `Season ${n} · ${sel.length} episodes`;
  }
  return `${sel.length} episodes across ${ss.size} seasons`;
}
