import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Check, ChevronRight, Download, ListChecks, Radar, RefreshCw, Trash2 } from "lucide-react";
import { api, type EpisodeAction, type EpisodeDetail, type Release, type SeriesDetail } from "@/lib/api";
import { autoGet, run } from "@/lib/actions";
import { bytes, epCode, pad } from "@/lib/format";
import { useGrabber, useLivePlan, type GrabOpts } from "@/lib/releases";
import { useRouter } from "@/lib/router";
import { useStore } from "@/lib/store";
import { useSearch } from "@/lib/useSearch";
import { cn } from "@/lib/utils";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator } from "@/components/ui/dropdown-menu";
import { Notice } from "@/components/kit";
import { useConfirm } from "@/components/Confirm";
import { FileLine, ManageMenu, MonitorToggle, RowMenu, SelectionBar } from "@/components/Manage";
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
  const confirm = useConfirm();
  const lib = d.inLibrary;
  // Monitoring flips show at once; the reload that follows confirms them.
  const [monOverride, setMonOverride] = useState<Map<number, boolean>>(new Map());
  useEffect(() => setMonOverride(new Map()), [d]);
  const isMon = (e: EpisodeDetail) => monOverride.get(e.id) ?? e.monitored;
  const anchor = useRef<number | null>(null);

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
          bypass: opts.bypass,
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
    const have = lib ? onDisk(selArr) : [];
    if (have.length) {
      const rest = selArr.filter((id) => !byId.get(id)?.hasFile);
      const all = have.length === selArr.length;
      const a = await confirm({
        title: all ? (have.length === 1 ? `${label(have[0].id)} is already on disk` : "These are already on disk") : `${have.length} of these are already on disk`,
        body: (
          <p>
            Replacing deletes the current {have.length === 1 ? "file" : `${have.length} files`} ({bytes(sizeOf(have))}) and downloads {have.length === 1 ? "it" : "them"} again.
          </p>
        ),
        confirm: have.length === 1 ? "Replace it" : `Replace ${have.length}`,
        alt: rest.length ? `Only get the ${rest.length} missing` : undefined,
        danger: true,
      });
      if (!a) return;
      if (a.action === "alt") {
        tickOnly(rest);
        setBusy(true);
        const res = await autoGet({ kind: "tv", seriesId: d.id, episodeIds: rest }, plural(rest.length, "missing episode"));
        setBusy(false);
        if (res) afterGrab();
        return;
      }
      if (!(await run(`Deleting ${describe(have)}…`, () => api.episodes(d.id, "delete", have.map((e) => e.id))))) return;
    }
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

  // Click ticks one episode; shift-click ticks (or unticks) the whole run
  // from the last one clicked, across seasons.
  const toggle = (e: EpisodeDetail, range = false) => {
    const n = new Set(scope === "custom" ? ticked : sel);
    const on = !n.has(e.id);
    let ids = [e.id];
    if (range && anchor.current !== null) {
      const order = allEps.filter((x) => x.aired).map((x) => x.id);
      const i = order.indexOf(anchor.current);
      const j = order.indexOf(e.id);
      if (i >= 0 && j >= 0) ids = order.slice(Math.min(i, j), Math.max(i, j) + 1);
    }
    for (const id of ids) {
      if (on) n.add(id);
      else n.delete(id);
    }
    anchor.current = e.id;
    setTicked(n);
    setScope("custom");
  };
  const tickOnly = (ids: number[]) => {
    setTicked(new Set(ids));
    setScope("custom");
  };
  const clearTicks = useCallback(() => {
    setTicked(new Set());
    anchor.current = null;
  }, []);
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

  // ---- Managing what is on disk -------------------------------------------

  const onDisk = (ids: number[]) => ids.map((id) => byId.get(id)).filter((e): e is EpisodeDetail => !!e?.hasFile);
  const sizeOf = (eps: EpisodeDetail[]) => eps.reduce((n, e) => n + (e.file?.size ?? 0), 0);
  const describe = (eps: EpisodeDetail[]) => (eps.length === 1 ? label(eps[0].id) : plural(eps.length, "episode"));
  const changed = () => {
    reload();
    reloadLibrary();
  };

  const episodesDo = async (action: EpisodeAction, ids: number[], pending: string) => {
    if (await run(pending, () => api.episodes(d.id, action, ids))) changed();
  };

  const setMonitored = (ids: number[], on: boolean) => {
    setMonOverride((m) => new Map([...m, ...ids.map((id) => [id, on] as const)]));
    episodesDo(on ? "monitor" : "unmonitor", ids, on ? "Monitoring…" : "Unmonitoring…");
  };

  const setSeasonMonitored = async (num: number, on: boolean) => {
    const ids = seasons.find((s) => s.number === num)?.episodes.map((e) => e.id) ?? [];
    setMonOverride((m) => new Map([...m, ...ids.map((id) => [id, on] as const)]));
    const ok = await run(
      on ? `Monitoring season ${num}…` : `Unmonitoring season ${num}…`,
      async () => {
        await api.patchTitle("tv", d.id, { monitored: on, season: num });
        return api.episodes(d.id, on ? "monitor" : "unmonitor", ids);
      },
      on ? `Sonarr watches season ${num} again` : `Sonarr stopped watching season ${num}`,
    );
    if (ok) changed();
  };

  const deleteFiles = async (ids: number[]) => {
    const eps = onDisk(ids);
    if (!eps.length) return;
    const a = await confirm({
      title: `Delete ${describe(eps)} from disk?`,
      body: (
        <p>
          Frees {bytes(sizeOf(eps))}. Sonarr moves files to its recycle bin when one is set up; otherwise they are gone for good.
        </p>
      ),
      options: [{ id: "unmonitor", label: "Stop monitoring them", hint: "so Sonarr does not download them again by itself", checked: true }],
      confirm: eps.length === 1 ? "Delete file" : `Delete ${eps.length} files`,
      danger: true,
    });
    if (!a) return;
    const ok = await run(`Deleting ${describe(eps)}…`, async () => {
      const res = await api.episodes(d.id, "delete", eps.map((e) => e.id));
      if (a.checked.unmonitor) await api.episodes(d.id, "unmonitor", eps.map((e) => e.id));
      return res;
    });
    if (ok) {
      clearTicks();
      changed();
    }
  };

  // Replace = delete what is there, then get it again: roarr's best pick,
  // or the release sheet to choose from.
  const replace = async (ids: number[], pick = false) => {
    const eps = onDisk(ids);
    if (!eps.length) return;
    const a = await confirm({
      title: `Replace ${describe(eps)}?`,
      body: (
        <p>
          roarr deletes the current {eps.length === 1 ? "file" : "files"} ({bytes(sizeOf(eps))}) and downloads {eps.length === 1 ? "it" : "them"} again.
        </p>
      ),
      options: [{ id: "pick", label: "Let me choose the release", hint: "otherwise roarr grabs its best pick", checked: pick }],
      confirm: "Replace",
      danger: true,
    });
    if (!a) return;
    if (!(await run(`Deleting ${describe(eps)}…`, () => api.episodes(d.id, "delete", eps.map((e) => e.id))))) return;
    tickOnly(ids);
    if (a.checked.pick) {
      setSheet(true);
      changed();
      return;
    }
    const res = await autoGet({ kind: "tv", seriesId: d.id, episodeIds: ids }, describe(eps));
    if (res) clearTicks();
    afterGrab();
  };

  const getNow = async (ids: number[]) => {
    tickOnly(ids);
    const res = await autoGet({ kind: "tv", seriesId: lib ? d.id : 0, tvdbId: d.tvdbId, tmdbId: d.tmdbId, episodeIds: ids }, ids.length === 1 ? label(ids[0]) : plural(ids.length, "episode"));
    if (res) {
      clearTicks();
      afterGrab(res.grabs?.find((g) => g.result.ok)?.result.seriesId);
    }
  };

  const choose = (ids: number[]) => {
    tickOnly(ids);
    setSheet(true);
  };

  const tickedArr = scope === "custom" ? selArr : [];
  const tickedOnDisk = onDisk(tickedArr);
  const tickedMissing = tickedArr.filter((id) => {
    const e = byId.get(id);
    return e && !e.hasFile && !e.queued;
  });
  const allMon = tickedArr.every((id) => {
    const e = byId.get(id);
    return e ? isMon(e) : true;
  });

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
            actions={
              lib && (
                <ManageMenu kind="tv" id={d.id} title={d.title} monitored={d.monitored} profileId={d.qualityProfileId} path={d.path} onChanged={changed}>
                  <DropdownMenuItem disabled={have === 0} onSelect={() => deleteFiles(allEps.filter((e) => e.hasFile).map((e) => e.id))}>
                    <Trash2 /> Delete every file…
                  </DropdownMenuItem>
                </ManageMenu>
              )
            }
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
            note={lib && onDisk(selArr).length > 0 ? `${plural(onDisk(selArr).length, "episode")} of these ${onDisk(selArr).length === 1 ? "is" : "are"} on disk already; you can replace or skip ${onDisk(selArr).length === 1 ? "it" : "them"}.` : undefined}
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
              const seasonName = s.number === 0 ? "Specials" : `Season ${s.number}`;
              const seasonMon = s.episodes.length > 0 ? s.episodes.some(isMon) : s.monitored;
              const seasonMissing = aired.filter((e) => !e.hasFile && !e.queued).map((e) => e.id);
              const seasonSize = s.episodes.reduce((n, e) => n + (e.file?.size ?? 0), 0);
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
                      <b className="text-[17px] font-semibold tracking-tight whitespace-nowrap">{s.number === 0 ? "Specials" : `Season ${s.number}`}</b>
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
                    {lib && (
                      <>
                        <MonitorToggle on={seasonMon} label={seasonName} onToggle={() => setSeasonMonitored(s.number, !seasonMon)} />
                        <RowMenu label={`${seasonName} actions`}>
                          <DropdownMenuLabel className="font-mono text-[11px] font-normal text-muted-foreground">
                            {seasonName} · {haveS}/{aired.length} on disk{seasonSize ? ` · ${bytes(seasonSize)}` : ""}
                          </DropdownMenuLabel>
                          <DropdownMenuItem disabled={aired.length === 0} onSelect={() => tickOnly(aired.map((e) => e.id))}>
                            <ListChecks /> Tick only this season
                          </DropdownMenuItem>
                          <DropdownMenuItem disabled={seasonMissing.length === 0} onSelect={() => getNow(seasonMissing)}>
                            <Download /> Get the {seasonMissing.length || ""} missing
                          </DropdownMenuItem>
                          <DropdownMenuItem disabled={haveS === 0} onSelect={() => replace(aired.map((e) => e.id))}>
                            <RefreshCw /> Replace the season…
                          </DropdownMenuItem>
                          <DropdownMenuItem disabled={seasonMissing.length === 0} onSelect={() => episodesDo("search", seasonMissing, "Asking Sonarr…")}>
                            <Radar /> Let Sonarr search
                          </DropdownMenuItem>
                          <DropdownMenuSeparator />
                          <DropdownMenuItem variant="destructive" disabled={haveS === 0} onSelect={() => deleteFiles(s.episodes.map((e) => e.id))}>
                            <Trash2 /> Delete season files…
                          </DropdownMenuItem>
                        </RowMenu>
                      </>
                    )}
                  </div>
                  {isOpen && s.episodes.length > 0 && (
                    <div className="pb-3">
                      {s.episodes.map((e) => {
                        const on = sel.has(e.id);
                        const code = label(e.id);
                        return (
                          <div
                            key={e.id}
                            onClick={(ev) => e.aired && toggle(e, ev.shiftKey)}
                            className={cn(
                              "group -mx-2.5 flex min-h-11 cursor-pointer items-center gap-3 rounded-lg px-2.5 py-1 transition-colors select-none hover:bg-muted/60",
                              on && "bg-lime/45 hover:bg-lime/55 dark:bg-lime/12 dark:hover:bg-lime/18",
                              !e.aired && "cursor-default",
                            )}
                          >
                            <Checkbox
                              checked={on}
                              disabled={!e.aired}
                              onClick={(ev) => ev.preventDefault()}
                              aria-label={`${code} ${e.title}`}
                              className={cn(!e.aired && "opacity-40")}
                            />
                            <span className={cn("w-9 shrink-0 font-mono text-xs text-muted-foreground", !e.aired && "opacity-55")}>
                              {e.absolute && d.anime ? `#${e.absolute}` : `E${pad(e.number)}`}
                            </span>
                            <span className={cn("min-w-0 flex-1", !e.aired && "opacity-55")}>
                              <span className="block truncate">{e.title}</span>
                              {e.file && <FileLine file={e.file} className="hidden sm:block" />}
                            </span>
                            {e.hasFile ? (
                              <span className="font-mono text-[11px] tracking-wide whitespace-nowrap text-ok uppercase">
                                ✓<span className="max-xl:hidden"> on disk</span>
                              </span>
                            ) : e.queued ? (
                              <Badge variant="ice">Downloading</Badge>
                            ) : e.aired ? (
                              <Badge variant="pink">Missing</Badge>
                            ) : (
                              <span className="font-mono text-[11px] text-muted-foreground">{e.airDate ?? "TBA"}</span>
                            )}
                            {lib && <MonitorToggle on={isMon(e)} label={code} onToggle={() => setMonitored([e.id], !isMon(e))} />}
                            {e.aired ? (
                              <RowMenu label={`${code} actions`} className="opacity-60 group-hover:opacity-100 focus-visible:opacity-100">
                                <DropdownMenuLabel className="font-mono text-[11px] font-normal">
                                  {code}
                                  {e.file && <span className="mt-0.5 block break-all text-muted-foreground">{e.file.path}</span>}
                                </DropdownMenuLabel>
                                {e.hasFile ? (
                                  <>
                                    <DropdownMenuItem onSelect={() => replace([e.id])}>
                                      <RefreshCw /> Replace…
                                    </DropdownMenuItem>
                                    <DropdownMenuItem onSelect={() => replace([e.id], true)}>
                                      <ListChecks /> Choose a replacement…
                                    </DropdownMenuItem>
                                  </>
                                ) : (
                                  <>
                                    <DropdownMenuItem onSelect={() => getNow([e.id])}>
                                      <Download /> Get it now
                                    </DropdownMenuItem>
                                    <DropdownMenuItem onSelect={() => choose([e.id])}>
                                      <ListChecks /> Choose release…
                                    </DropdownMenuItem>
                                  </>
                                )}
                                {lib && (
                                  <DropdownMenuItem onSelect={() => episodesDo("search", [e.id], "Asking Sonarr…")}>
                                    <Radar /> Let Sonarr search
                                  </DropdownMenuItem>
                                )}
                                {e.hasFile && (
                                  <>
                                    <DropdownMenuSeparator />
                                    <DropdownMenuItem variant="destructive" onSelect={() => deleteFiles([e.id])}>
                                      <Trash2 /> Delete file…
                                    </DropdownMenuItem>
                                  </>
                                )}
                              </RowMenu>
                            ) : (
                              <span className="w-7 shrink-0" />
                            )}
                          </div>
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
      <SelectionBar
        count={tickedArr.length}
        onClear={clearTicks}
        summary={tickedOnDisk.length > 0 && `${tickedOnDisk.length} on disk · ${bytes(sizeOf(tickedOnDisk))}`}
      >
        {tickedMissing.length > 0 && (
          <Button size="sm" className="shrink-0" onClick={() => getNow(tickedMissing)}>
            <Download /> Get {tickedMissing.length === tickedArr.length ? "" : `${tickedMissing.length} missing`}
          </Button>
        )}
        <Button variant="ghost" size="sm" className="shrink-0" onClick={() => setSheet(true)}>
          <ListChecks /> Choose
        </Button>
        {lib && tickedOnDisk.length > 0 && (
          <Button variant="ghost" size="sm" className="shrink-0" onClick={() => replace(tickedOnDisk.map((e) => e.id))}>
            <RefreshCw /> Replace{tickedOnDisk.length < tickedArr.length ? ` ${tickedOnDisk.length}` : ""}
          </Button>
        )}
        {lib && (
          <Button variant="ghost" size="sm" className="shrink-0" onClick={() => setMonitored(tickedArr, !allMon)}>
            {allMon ? "Unmonitor" : "Monitor"}
          </Button>
        )}
        {lib && tickedOnDisk.length > 0 && (
          <Button variant="ghost" size="sm" className="shrink-0 text-destructive hover:text-destructive" onClick={() => deleteFiles(tickedArr)}>
            <Trash2 /> Delete{tickedOnDisk.length < tickedArr.length ? ` ${tickedOnDisk.length}` : ""}
          </Button>
        )}
      </SelectionBar>
    </>
  );
}

const plural = (n: number, word: string) => `${n} ${word}${n === 1 ? "" : "s"}`;

/** "S02E08 – S02E10" for a sorted list of episode codes. */
function rangeLabel(codes: string[]): string {
  if (codes.length === 0) return "";
  if (codes.length === 1) return codes[0];
  return `${codes[0]} – ${codes[codes.length - 1]}`;
}
