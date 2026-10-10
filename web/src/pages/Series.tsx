import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Check, ChevronRight } from "lucide-react";
import { api, type EpisodeDetail, type Release, type SeriesDetail } from "@/lib/api";
import { autoGet } from "@/lib/actions";
import { bytes, epCode, pad } from "@/lib/format";
import { useGrabber, useLivePlan, type GrabOpts } from "@/lib/releases";
import { useRouter } from "@/lib/router";
import { useStore } from "@/lib/store";
import { useSearch } from "@/lib/useSearch";
import { cn } from "@/lib/utils";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Notice } from "@/components/kit";
import { ReleaseSheet } from "@/components/ReleaseSheet";
import { Crumbs, GetCard, PosterColumn, TitleHead, TitleLayout, TitleSkeleton, type GetOption } from "@/components/Title";

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

  if (error) return <Notice tone="bad">{error}</Notice>;
  if (!d) return <TitleSkeleton />;
  return <SeriesView d={d} reload={() => load(true)} prefs={prefs} />;
}

type Scope = "missing" | "all" | "season" | "custom";

function SeriesView({ d, reload, prefs }: { d: SeriesDetail; reload: () => void; prefs: { prefetch: boolean; watchFirst: boolean } }) {
  const { reloadActivity, reloadLibrary } = useStore();
  const { navigate, search: params } = useRouter();
  const seasons = useMemo(() => d.seasons.filter((s) => s.episodes.length > 0 || d.synthetic), [d]);
  const allEps = useMemo(() => seasons.flatMap((s) => s.episodes), [seasons]);
  const byId = useMemo(() => new Map(allEps.map((e) => [e.id, e])), [allEps]);
  const missing = useMemo(() => allEps.filter((e) => e.season > 0 && e.aired && !e.hasFile && !e.queued).map((e) => e.id), [allEps]);
  const everything = useMemo(() => allEps.filter((e) => e.season > 0 && e.aired).map((e) => e.id), [allEps]);

  // The season offered as a one-click option: where the gaps start, or the
  // latest season; for shows not in Sonarr yet, the first one.
  const focusSeason = useMemo(() => {
    const regular = seasons.filter((s) => s.number > 0);
    if (!d.inLibrary) return regular[0]?.number ?? seasons[0]?.number ?? 1;
    const gap = missing.length ? byId.get(missing[0])?.season : undefined;
    return gap ?? regular[regular.length - 1]?.number ?? 1;
  }, [seasons, missing, byId, d.inLibrary]);

  const seasonWanted = useCallback(
    (n: number) => {
      const s = seasons.find((x) => x.number === n);
      if (!s) return [];
      const aired = s.episodes.filter((e) => e.aired);
      const miss = aired.filter((e) => !e.hasFile && !e.queued);
      return (miss.length ? miss : aired).map((e) => e.id);
    },
    [seasons],
  );

  const [scope, setScope] = useState<Scope>(() => {
    if (!d.inLibrary) return seasons.filter((s) => s.number > 0).length > 1 ? "season" : "all";
    return missing.length ? "missing" : "custom";
  });
  const [ticked, setTicked] = useState<Set<number>>(new Set());
  const [openSeasons, setOpenSeasons] = useState<Set<number>>(() => new Set(seasons.length === 1 ? [seasons[0].number] : [focusSeason]));
  const [sheet, setSheet] = useState(params.get("pick") === "1");
  const [watchFirst, setWatchFirst] = useState(prefs.watchFirst);
  const [busy, setBusy] = useState(false);
  const [gotFor, setGotFor] = useState<string | null>(null);
  useEffect(() => setWatchFirst(prefs.watchFirst), [prefs.watchFirst]);

  const selArr = useMemo(() => {
    const ids = scope === "missing" ? missing : scope === "all" ? everything : scope === "season" ? seasonWanted(focusSeason) : [...ticked];
    return [...ids].sort((a, b) => a - b);
  }, [scope, missing, everything, seasonWanted, focusSeason, ticked]);
  const sel = useMemo(() => new Set(selArr), [selArr]);
  const selKey = selArr.join(",");

  const ref: Record<string, string> = d.inLibrary ? { id: String(d.id) } : { tvdb: String(d.tvdbId ?? 0), tmdb: String(d.tmdbId ?? 0) };

  // Streams: the whole show (prefetched as soon as the page opens) plus
  // exact episode queries when only a few episodes are selected.
  const streams = useMemo(() => {
    const out: Record<string, string>[] = [];
    if (prefs.prefetch || selArr.length > 3) out.push({ kind: "tv", ...ref, show: "1" });
    if (selArr.length > 0 && selArr.length <= 3) {
      out.push({ kind: "tv", ...ref, episodes: selArr.filter((x) => x > 0).join(",") });
      if (!prefs.prefetch) {
        const ss = [...new Set(selArr.map((x) => byId.get(x)?.season).filter((x) => x !== undefined))];
        out.push({ kind: "tv", ...ref, seasons: ss.join(",") });
      }
    }
    return out.filter((s) => s.episodes !== "");
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selKey, prefs.prefetch, d.key]);
  const search = useSearch(d.key, streams);

  const relevant = useCallback(
    (r: Release) => {
      if (!r.covers || r.covers.length === 0) return d.synthetic === true && r.match;
      return r.covers.some((id) => sel.has(id));
    },
    [sel, d.synthetic],
  );
  const plan = useLivePlan(search, relevant, selArr, "tv");

  const later = useRef(0);
  const afterGrab = (seriesId?: number) => {
    reloadActivity();
    clearTimeout(later.current);
    later.current = window.setTimeout(() => {
      if (!d.inLibrary && seriesId) {
        reloadLibrary();
        navigate(`/tv/${seriesId}`, { replace: true });
      } else reload();
    }, 1200);
  };

  const grabber = useGrabber(
    useCallback(
      async (r: Release, opts: GrabOpts) => {
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
      },
      // eslint-disable-next-line react-hooks/exhaustive-deps
      [selKey, d],
    ),
  );

  const label = (id: number) => {
    const e = byId.get(id);
    if (!e) return `#${id}`;
    return d.anime && e.absolute ? `#${e.absolute}` : epCode(e.season, e.number);
  };

  // One button: grab exactly the plan we are showing once every indexer has
  // answered; until then let the server wait for a stable plan.
  const get = async () => {
    setBusy(true);
    try {
      if (search.done && plan.picks.length) {
        const ok = await grabber.grabAll(
          plan.picks.map((x) => x.release),
          { watchFirst },
        );
        if (ok) setGotFor(selKey);
      } else {
        const res = await autoGet({ kind: "tv", seriesId: d.inLibrary ? d.id : 0, tvdbId: d.tvdbId, tmdbId: d.tmdbId, episodeIds: selArr }, scopeTitle);
        if (res) {
          setGotFor(selKey);
          afterGrab(res.grabs?.find((g) => g.result.ok)?.result.seriesId);
        }
      }
    } finally {
      setBusy(false);
    }
  };

  const toggle = (e: EpisodeDetail) => {
    const n = new Set(scope === "custom" ? ticked : sel);
    if (n.has(e.id)) n.delete(e.id);
    else n.add(e.id);
    setTicked(n);
    setScope("custom");
  };
  const tickSeason = (num: number) => {
    const ids = seasons.find((s) => s.number === num)?.episodes.filter((e) => e.aired).map((e) => e.id) ?? [];
    const all = ids.every((x) => sel.has(x));
    const n = new Set(scope === "custom" ? ticked : sel);
    for (const x of ids) {
      if (all) n.delete(x);
      else n.add(x);
    }
    setTicked(n);
    setScope("custom");
  };

  const have = allEps.filter((e) => e.season > 0 && e.hasFile).length;
  const queued = allEps.filter((e) => e.season > 0 && e.queued).length;
  const n = selArr.length;
  const scopeTitle =
    scope === "all" ? "the whole show" : scope === "missing" ? `${n} missing episode${n === 1 ? "" : "s"}` : n === 1 ? label(selArr[0]) : `${n} episodes of ${d.title}`;
  const sheetTitle = n === 1 ? `${d.title} · ${label(selArr[0])}` : scope === "season" ? `${d.title} · Season ${focusSeason}` : `${d.title} · ${n} episodes`;
  const focusAired = seasons.find((s) => s.number === focusSeason)?.episodes.filter((e) => e.aired) ?? [];
  const focusMissing = focusAired.filter((e) => !e.hasFile && !e.queued).length;
  const regularSeasons = seasons.filter((s) => s.number > 0).length;

  const options: GetOption[] = [
    ...(d.inLibrary ? [{ value: "missing", label: "Missing episodes", sub: missing.length ? rangeLabel(missing.map(label)) : "nothing missing", count: missing.length, disabled: missing.length === 0 }] : []),
    ...(regularSeasons > 1 || !d.inLibrary
      ? [
          {
            value: "season",
            label: `Season ${focusSeason}`,
            sub: d.inLibrary && focusMissing ? `${focusMissing} missing of ${focusAired.length}` : `${focusAired.length} episodes`,
            count: seasonWanted(focusSeason).length,
          },
        ]
      : []),
    { value: "all", label: "Whole show", sub: `${regularSeasons} season${regularSeasons === 1 ? "" : "s"}`, count: everything.length },
    { value: "custom", label: "Episodes I tick", sub: "in the list below", count: scope === "custom" ? ticked.size || "–" : "–" },
  ];

  return (
    <>
      <Crumbs section="Shows" />
      <TitleLayout
        poster={<PosterColumn poster={d.poster} title={d.title} year={d.year} />}
        head={
          <TitleHead
            poster={d.poster}
            title={d.title}
            year={d.year}
            meta={["Show", d.network, d.year && `${d.year}–`, d.status, d.anime && "anime", d.runtime && `${d.runtime} min`]}
            overview={d.overview}
            badges={
              d.inLibrary ? (
                <>
                  <Badge variant="ok">
                    <Check /> {have}/{everything.length} on disk
                  </Badge>
                  {queued > 0 && <Badge variant="ice">{queued} downloading</Badge>}
                  {missing.length > 0 && <Badge variant="pink">{missing.length} missing</Badge>}
                  {d.sizeOnDisk ? <Badge>{bytes(d.sizeOnDisk)}</Badge> : null}
                </>
              ) : (
                <Badge variant="ice">Not in Sonarr yet · added when you get something</Badge>
              )
            }
          />
        }
        aside={
          <GetCard
            options={options}
            value={scope}
            onValueChange={(v) => {
              if (v === "custom" && scope !== "custom") setTicked(new Set(sel));
              setScope(v as Scope);
            }}
            cta={n === 0 ? "Tick episodes to get" : n === 1 ? `Get ${label(selArr[0])}` : `Get ${n} episodes`}
            onGet={get}
            busy={busy}
            done={gotFor === selKey}
            disabled={n === 0}
            state={search}
            plan={plan}
            wantedCount={n}
            onChoose={() => setSheet(true)}
            watchFirst={watchFirst}
            onWatchFirst={setWatchFirst}
          />
        }
        body={
          <div className="border-t border-dashed border-dash">
            {seasons.map((s) => {
              const aired = s.episodes.filter((e) => e.aired);
              const haveS = aired.filter((e) => e.hasFile).length;
              const isOpen = openSeasons.has(s.number);
              const complete = !d.synthetic && aired.length > 0 && haveS === aired.length;
              const allTicked = aired.length > 0 && aired.every((e) => sel.has(e.id));
              return (
                <section key={s.number} className="border-b border-dashed border-dash">
                  <div className="flex min-h-14 items-center gap-3">
                    <button
                      className="flex min-w-0 flex-1 cursor-pointer items-center gap-3 py-3 text-left outline-none focus-visible:underline"
                      aria-expanded={isOpen}
                      onClick={() => {
                        const next = new Set(openSeasons);
                        if (next.has(s.number)) next.delete(s.number);
                        else next.add(s.number);
                        setOpenSeasons(next);
                      }}
                    >
                      <ChevronRight className={cn("size-4 shrink-0 text-muted-foreground transition-transform", isOpen && "rotate-90")} />
                      <b className="text-[17px] font-semibold tracking-tight">{s.number === 0 ? "Specials" : `Season ${s.number}`}</b>
                      <span className="font-mono text-xs text-muted-foreground">{d.synthetic ? `${s.episodes.length} eps` : `${haveS}/${aired.length}`}</span>
                      {!d.synthetic && aired.length > 0 && (
                        <span className="hidden h-1 w-28 overflow-hidden rounded-full bg-foreground/10 sm:block">
                          <i className="block h-full rounded-full bg-foreground" style={{ width: `${(haveS / aired.length) * 100}%` }} />
                        </span>
                      )}
                    </button>
                    {complete && !allTicked ? (
                      <Badge variant="ok">Complete</Badge>
                    ) : (
                      aired.length > 0 && (
                        <Button variant="outline" size="xs" onClick={() => tickSeason(s.number)}>
                          {allTicked ? "Untick season" : "Tick season"}
                        </Button>
                      )
                    )}
                  </div>
                  {isOpen && s.episodes.length > 0 && (
                    <div className="pb-3">
                      {s.episodes.map((e) => {
                        const on = sel.has(e.id);
                        return (
                          <label
                            key={e.id}
                            className={cn(
                              "-mx-2.5 flex min-h-11 cursor-pointer items-center gap-3.5 rounded-lg px-2.5 transition-colors hover:bg-muted/60",
                              on && "bg-lime/45 hover:bg-lime/55 dark:bg-lime/12 dark:hover:bg-lime/18",
                              !e.aired && "cursor-default opacity-55",
                            )}
                          >
                            <Checkbox checked={on} disabled={!e.aired} onCheckedChange={() => toggle(e)} aria-label={`${label(e.id)} ${e.title}`} />
                            <span className="w-9 shrink-0 font-mono text-xs text-muted-foreground">{e.absolute && d.anime ? `#${e.absolute}` : `E${pad(e.number)}`}</span>
                            <span className="min-w-0 flex-1 truncate">{e.title}</span>
                            {e.hasFile ? (
                              <span className="font-mono text-[11px] tracking-wide text-ok uppercase">✓ on disk</span>
                            ) : e.queued ? (
                              <Badge variant="ice">Downloading</Badge>
                            ) : e.aired ? (
                              <Badge variant="pink">Missing</Badge>
                            ) : (
                              <span className="font-mono text-[11px] text-muted-foreground">{e.airDate ?? "TBA"}</span>
                            )}
                          </label>
                        );
                      })}
                    </div>
                  )}
                </section>
              );
            })}
          </div>
        }
      />
      <ReleaseSheet
        open={sheet}
        onOpenChange={setSheet}
        title={sheetTitle}
        detail={n > 0 && n <= 8 ? selArr.map(label).join(" · ") : n > 8 ? rangeLabel(selArr.map(label)) : undefined}
        kind="tv"
        state={search}
        plan={plan}
        wanted={selArr}
        grabber={grabber}
        watchFirst={watchFirst}
        epLabel={label}
      />
    </>
  );
}

/** "S02E08 – S02E10" for a sorted list of episode codes. */
function rangeLabel(codes: string[]): string {
  if (codes.length === 0) return "";
  if (codes.length === 1) return codes[0];
  return `${codes[0]} – ${codes[codes.length - 1]}`;
}
